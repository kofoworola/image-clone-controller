package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
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
	username       string
	password       string

	ignoreNs = []string{"kube-system"}
)

func init() {
	flag.StringVar(&registry, "registry", "kofoworola", "what registry will be used to make backups without the forward slash")
	flag.StringVar(
		&skipNamespaces,
		"ignore-namespaces",
		"",
		"comma separated list of namespaces to ignore. NOTE `kube-system` namesapces will always be ignored",
	)
	flag.StringVar(&username, "username", "kofoworola", "registry username")
	flag.StringVar(&password, "password", "", "registry password")

	log.SetLogger(zap.New())
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

func (r *imageReconciler) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	logger := log.FromContext(ctx)
	deploy := &appsv1.Deployment{}
	daemonSet := &appsv1.DaemonSet{}
	isDaemon := false
	err := r.client.Get(ctx, req.NamespacedName, deploy)
	if errors.IsNotFound(err) {
		isDaemon = true
		logger.Info("is not deployment, checking daemon")
		if err = r.client.Get(ctx, req.NamespacedName, daemonSet); errors.IsNotFound(err) {
			logger.Error(err, "not found")
			return reconcile.Result{}, nil
		}

	}

	if isDaemon {
		conts := daemonSet.Spec.Template.Spec.Containers
		modifiedContainers := make([]corev1.Container, len(conts))
		for i, container := range conts {
			newImage, err := r.backupImage(container.Image, ctx)
			if err != nil {
				logger.Error(err, "error backing up image", "image-name", container.Image)
				return reconcile.Result{}, nil
			}
			container.Image = newImage
			modifiedContainers[i] = container
		}
		daemonSet.Spec.Template.Spec.Containers = modifiedContainers
		if err := r.client.Update(ctx, daemonSet); err != nil {
			logger.Error(err, "error updating image")
		}
	} else {
		conts := deploy.Spec.Template.Spec.Containers
		modifiedContainers := make([]corev1.Container, len(conts))
		for i, container := range conts {
			newImage, err := r.backupImage(container.Image, ctx)
			if err != nil {
				logger.Error(err, "error backing up image", "image-name", container.Image)
				return reconcile.Result{}, nil
			}
			container.Image = newImage
			modifiedContainers[i] = container
		}
		deploy.Spec.Template.Spec.Containers = modifiedContainers
		if err := r.client.Update(ctx, deploy); err != nil {
			logger.Error(err, "error updating image")
		}

	}
	return reconcile.Result{}, nil
}

func (r *imageReconciler) backupImage(imageName string, ctx context.Context) (string, error) {
	logger := log.FromContext(ctx)

	// check if its a part of the registry already
	if strings.Contains(imageName, registry+"/") {
		return imageName, nil
	}

	reference, err := name.ParseReference(imageName)
	if err != nil {
		return "", fmt.Errorf("error parsing image reference: %w", err)
	}

	image, err := remote.Image(reference)
	if err != nil {
		return "", fmt.Errorf("error getting image: %w", err)
	}

	var newImageName string
	index := strings.LastIndex(imageName, "/")
	if index == -1 {
		newImageName = fmt.Sprintf("%s/%s", registry, imageName)
	} else {
		newImageName = fmt.Sprintf("%s/%s", registry, imageName[index+1:])
	}

	logger.Info("pushing to new registry", "image-name", newImageName)
	reference, err = name.ParseReference(newImageName)
	if err != nil {
		return "", fmt.Errorf("error creating reference for %s: %v", newImageName, err)
	}

	if err := remote.Write(reference, image, remote.WithAuth(&authn.Basic{
		Username: username,
		Password: password,
	})); err != nil {
		return "", fmt.Errorf("error writing to new registry %s: %v", newImageName, err)
	}

	return newImageName, nil
}
