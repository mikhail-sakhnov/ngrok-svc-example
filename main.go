package main

import (
	"context"
	"fmt"
	"os"

	stderr "errors"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func check(e error) {
	if e != nil {
		panic(e)
	}
}

const (
	ngrokLabel     = "ngrok"
	ngrokNamespace = "ngrok-tunnel"
	finalizerName  = "ngrok.io/tunnel"
)

func main() {
	token := os.Getenv("NGROK_TOKEN")
	if token == "" {
		panic("Please set-up NGROK_TOKEN env variable, see https://dashboard.ngrok.com/get-started/your-authtoken")
	}
	config, err := clientcmd.BuildConfigFromFlags("", os.Getenv("KUBECONFIG"))
	check(err)
	mgr, err := manager.New(config, manager.Options{
		Logger: zap.New(zap.UseDevMode(true)), // let's be verbose
	})
	check(err)
	l := mgr.GetLogger()

	var svc corev1.Service

	err = builder.
		ControllerManagedBy(mgr).
		For(&corev1.Service{}).
		Owns(&corev1.Pod{}).
		Complete(reconcile.Func(func(ctx context.Context, r reconcile.Request) (reconcile.Result, error) {
			l.Info("Reconciling", "key", r)
			c := mgr.GetClient()
			if err := c.Get(ctx, r.NamespacedName, &svc); err != nil {
				if errors.IsNotFound(err) {
					// no need to reconcile if not found
					return reconcile.Result{}, nil
				}
				l.Error(err, "Can't load service")
				// let the controller-runtime deal with backoff logic
				return reconcile.Result{}, err
			}
			if label := svc.Labels[ngrokLabel]; label != "true" {
				l.Info("svc has no ngrok label", "key", r)
				return reconcile.Result{}, nil
			}
			if !svc.DeletionTimestamp.IsZero() {
				// cleanup
				if err := deleteTunnelPod(ctx, c, svc); err != nil {
					l.Error(err, "Can't delete service")

					return reconcile.Result{}, err
				}
				controllerutil.RemoveFinalizer(&svc, finalizerName)
				if err := c.Update(ctx, &svc); err != nil {
					return reconcile.Result{}, err
				}
				return reconcile.Result{Requeue: true}, nil
			} else {
				l.Info("creating tunnel for service", "key", r)
				controllerutil.AddFinalizer(&svc, finalizerName)
				if err := c.Update(ctx, &svc); err != nil {
					return reconcile.Result{}, err
				}

				if err := ensureTunnelPod(ctx, c, svc, token); err != nil {
					l.Error(err, "error exposing tunnel")
					return reconcile.Result{}, err
				}
				return reconcile.Result{}, nil
			}
		}))
	check(err)
	check(mgr.Start(context.Background()))
}

func makeLabels(svc corev1.Service) map[string]string {
	return map[string]string{
		"exposed-from": getLinkedName(svc),
	}
}

func deleteTunnelPod(ctx context.Context, c client.Client, svc corev1.Service) error {
	pods := &corev1.PodList{}
	if err := c.List(ctx, pods, client.InNamespace(ngrokNamespace)); err != nil {
		return err
	}
	if len(pods.Items) == 0 {
		return fmt.Errorf("can't find dependent pod")
	}
	if err := c.Delete(ctx, &pods.Items[0]); err != nil {
		return err
	}
	return nil
}

func ensureTunnelPod(ctx context.Context, c client.Client, svc corev1.Service, token string) error {
	target := fmt.Sprintf("%s:%d", svc.Spec.ClusterIP, svc.Spec.Ports[0].Port)
	spec := getPodSpec(target, svc, token)
	pods := &corev1.PodList{}
	if err := c.List(ctx, pods, client.InNamespace(ngrokNamespace), client.MatchingLabels(makeLabels(svc))); err != nil {
		return err
	}
	if len(pods.Items) > 0 {
		return nil
	}
	if err := c.Create(ctx, &spec); err != nil {
		var statusErr *errors.StatusError
		if stderr.As(err, &statusErr) && statusErr.ErrStatus.Reason == v1.StatusReasonAlreadyExists {
			// do nothing on resync or controller restart
			return nil
		}
		return err
	}
	return nil
}

func getLinkedName(svc corev1.Service) string {
	return fmt.Sprintf("ns-%s-svc-%s", svc.Namespace, svc.Name)
}

func getPodSpec(target string, svc corev1.Service, token string) corev1.Pod {
	uid := int64(0)
	return corev1.Pod{
		TypeMeta: v1.TypeMeta{Kind: "Pod", APIVersion: "v1"},
		ObjectMeta: v1.ObjectMeta{
			Namespace:    ngrokNamespace,
			GenerateName: "ngrok-tunnel-",
			Labels:       makeLabels(svc),
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:  "tunnel",
					Image: "docker.io/soider/ngrok-tunnel-pod",
					SecurityContext: &corev1.SecurityContext{
						RunAsUser: &uid,
					},
					Command:         []string{"ngrok"},
					Args:            []string{"http", target, "--log", "stdout", "--authtoken", token},
					ImagePullPolicy: corev1.PullAlways,
				},
			},
		},
	}
}
