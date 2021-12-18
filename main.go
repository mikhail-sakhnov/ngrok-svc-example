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
)

func main() {
	token := os.Getenv("NGROK_TOKEN")
	if token == "" {
		panic("Please set-up NGROK_TOKEN env variable, see https://dashboard.ngrok.com/get-started/your-authtoken")
	}
	config, err := clientcmd.BuildConfigFromFlags("", os.Getenv("KUBECONFIG"))
	check(err)
	mgr, err := manager.New(config, manager.Options{
		Logger: zap.New(), // let's be verbose
	})
	check(err)
	l := mgr.GetLogger()

	var svc corev1.Service

	err = builder.
		ControllerManagedBy(mgr).
		For(&corev1.Service{}).
		Owns(&corev1.Pod{}).
		Complete(reconcile.Func(func(c context.Context, r reconcile.Request) (reconcile.Result, error) {
			l.Info("Reconciling", "key", r)
			if err := mgr.GetClient().Get(c, r.NamespacedName, &svc); err != nil {
				l.Error(err, "Can't load service")
				if errors.IsNotFound(err) {
					// no need to reconcile if not found
					return reconcile.Result{}, nil
				}
				// let the controller-runtime deal with backoff logic
				return reconcile.Result{}, err
			}

			if label := svc.Labels[ngrokLabel]; label != "true" {
				l.Info("svc has no ngrok label", "key", r)
				return reconcile.Result{}, nil
			}
			l.Info("creating tunnel for service", "key", r)
			if err := ensureTunnelPod(c, mgr.GetClient(), svc, token); err != nil {
				l.Error(err, "error exposing tunnel")
				return reconcile.Result{}, err
			}
			return reconcile.Result{}, nil
		}))
	check(err)
	check(mgr.Start(context.Background()))
}

func ensureTunnelPod(ctx context.Context, client client.Client, svc corev1.Service, token string) error {
	target := fmt.Sprintf("%s:%d", svc.Spec.ClusterIP, svc.Spec.Ports[0].Port)
	spec := getPodSpec(target, getLinkedName(svc), token)
	if err := client.Create(ctx, &spec); err != nil {
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

func getPodSpec(target string, name string, token string) corev1.Pod {
	uid := int64(0)
	return corev1.Pod{
		TypeMeta:   v1.TypeMeta{Kind: "Pod", APIVersion: "v1"},
		ObjectMeta: v1.ObjectMeta{Name: name, Namespace: ngrokNamespace},
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
