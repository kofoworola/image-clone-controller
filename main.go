package main

import (
	"context"
	"flag"
	"os"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/manager/signals"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

//TODO check preexisting items

var (
	registry       string
	skipNamespaces string

	ignoreNs = []string{"kube-system"}
)

func init() {
	flag.StringVar(&registry, "registry", "", "what registry will be used to make backups")
	flag.StringVar(
		&skipNamespaces,
		"ignore-namespaces",
		"",
		"comma separated list of namespaces to ignore. NOTE `kube-system` namesapces will always be ignored",
	)

}
func main() {
	flag.Parse()
	// add list of namespaces to ignore
	if skipNamespaces != "" {
		ignoreNs = append(ignoreNs, strings.Split(skipNamespaces, ",")...)
	}

	logger := log.Log.WithName("image-clone-controller")

	logger.Info("setting up the manager")
	mng, err := manager.New(config.GetConfigOrDie(), manager.Options{})
	if err != nil {
		logger.Error(err, "error creating manager")
		// exit with non okay code so the container knows to fail
		os.Exit(1)
	}

	logger.Info("creating the controller")
	controller, err := controller.New(
		"image-clone-controller",
		mng,
		controller.Options{
			Reconciler: &imageReconciler{mng.GetClient()},
			Log:        logger,
		},
	)
	if err != nil {
		logger.Error(err, "error creating reconciler")
		os.Exit(1)
	}

	// watch deployments and daemonsets using a predicate that filters based on namespace
	controller.Watch(
		&source.Kind{Type: &appsv1.Deployment{}},
		&handler.EnqueueRequestForObject{},
		predicate.NewPredicateFuncs(func(object client.Object) bool {
			deploy := object.(*appsv1.Deployment)
			for _, ns := range ignoreNs {
				if deploy.Namespace == ns {
					return false
				}
			}
			return true
		}),
	)

	controller.Watch(
		&source.Kind{Type: &appsv1.DaemonSet{}},
		&handler.EnqueueRequestForObject{},
		predicate.NewPredicateFuncs(func(object client.Object) bool {
			ds := object.(*appsv1.DaemonSet)
			for _, ns := range ignoreNs {
				if ds.Namespace == ns {
					return false
				}
			}
			return true

		}),
	)

	logger.Info("starting the manager")
	if err := mng.Start(signals.SetupSignalHandler()); err != nil {
		logger.Error(err, "unable to run manager")
		os.Exit(1)
	}
}

type imageReconciler struct {
	client client.Client
}

func (r imageReconciler) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("received reconcile request")
	return reconcile.Result{}, nil
}
