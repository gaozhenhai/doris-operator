package k8s

import (
	"context"
	"errors"
	"fmt"

	dorisv1 "github.com/selectdb/doris-operator/api/doris/v1"
	appv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/autoscaling/v1"
	v2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// judge two services equal or not in some fields. develoer can custom the function.
type ServiceEqual func(svc1 *corev1.Service, svc2 *corev1.Service) bool

// judge two statefulset equal or not in some fields. develoer can custom the function.
type StatefulSetEqual func(st1 *appv1.StatefulSet, st2 *appv1.StatefulSet) bool

func ApplyService(ctx context.Context, k8sclient client.Client, svc *corev1.Service, equal ServiceEqual) error {
	// As stated in the RetryOnConflict's documentation, the returned error shouldn't be wrapped.
	var esvc corev1.Service
	err := k8sclient.Get(ctx, types.NamespacedName{Name: svc.Name, Namespace: svc.Namespace}, &esvc)
	if err != nil && apierrors.IsNotFound(err) {
		return CreateClientObject(ctx, k8sclient, svc)
	} else if err != nil {
		return err
	}

	if equal(svc, &esvc) {
		klog.Info("CreateOrUpdateService service Name, Ports, Selector, ServiceType, Labels have not change ", "namespace ", svc.Namespace, " name ", svc.Name)
		return nil
	}

	return PatchClientObject(ctx, k8sclient, svc)
}

// ApplyStatefulSet when the object is not exist, create object. if exist and statefulset have been updated, patch the statefulset.
func ApplyStatefulSet(ctx context.Context, k8sclient client.Client, st *appv1.StatefulSet, equal StatefulSetEqual) error {
	var est appv1.StatefulSet
	err := k8sclient.Get(ctx, types.NamespacedName{Namespace: st.Namespace, Name: st.Name}, &est)
	if err != nil && apierrors.IsNotFound(err) {
		return CreateClientObject(ctx, k8sclient, st)
	} else if err != nil {
		return err
	}

	//if have restart annotation we should exclude it impacts on hash.
	if equal(st, &est) {
		klog.Infof("ApplyStatefulSet Sync exist statefulset name=%s, namespace=%s, equals to new statefulset.", est.Name, est.Namespace)
		return nil
	}

	st.ResourceVersion = est.ResourceVersion
	return PatchClientObject(ctx, k8sclient, st)
}

func CreateClientObject(ctx context.Context, k8sclient client.Client, object client.Object) error {
	klog.Info("Creating resource service ", "namespace ", object.GetNamespace(), " name ", object.GetName(), " kind ", object.GetObjectKind().GroupVersionKind().Kind)
	if err := k8sclient.Create(ctx, object); err != nil {
		return err
	}
	return nil
}

func UpdateClientObject(ctx context.Context, k8sclient client.Client, object client.Object) error {
	klog.Info("Updating resource service ", "namespace ", object.GetNamespace(), " name ", object.GetName(), " kind ", object.GetObjectKind())
	if err := k8sclient.Update(ctx, object); err != nil {
		return err
	}
	return nil
}

func CreateOrUpdateClientObject(ctx context.Context, k8sclient client.Client, object client.Object) error {
	klog.V(4).Infof("create or update resource namespace=%s,name=%s,kind=%s.", object.GetNamespace(), object.GetName(), object.GetObjectKind())
	if err := k8sclient.Update(ctx, object); apierrors.IsNotFound(err) {
		return k8sclient.Create(ctx, object)
	} else if err != nil {
		return err
	}

	return nil
}

// PatchClientObject patch object when the object exist. if not return error.
func PatchClientObject(ctx context.Context, k8sclient client.Client, object client.Object) error {
	klog.V(4).Infof("patch resource namespace=%s,name=%s,kind=%s.", object.GetNamespace(), object.GetName(), object.GetObjectKind())
	if err := k8sclient.Patch(ctx, object, client.Merge); err != nil {
		return err
	}

	return nil
}

// PatchOrCreate patch object if not exist create object.
func PatchOrCreate(ctx context.Context, k8sclient client.Client, object client.Object) error {
	klog.V(4).Infof("patch or create resource namespace=%s,name=%s,kind=%s.", object.GetNamespace(), object.GetName(), object.GetObjectKind())
	if err := k8sclient.Patch(ctx, object, client.Merge); apierrors.IsNotFound(err) {
		return k8sclient.Create(ctx, object)
	} else if err != nil {
		return err
	}

	return nil
}

func DeleteClientObject(ctx context.Context, k8sclient client.Client, object client.Object) error {
	if err := k8sclient.Delete(ctx, object); err != nil {
		return err
	}
	return nil
}

// DeleteStatefulset delete statefulset.
func DeleteStatefulset(ctx context.Context, k8sclient client.Client, namespace, name string) error {
	var st appv1.StatefulSet
	if err := k8sclient.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, &st); apierrors.IsNotFound(err) {
		return nil
	} else if err != nil {
		return err
	}

	return k8sclient.Delete(ctx, &st)
}

// DeleteService delete service.
func DeleteService(ctx context.Context, k8sclient client.Client, namespace, name string) error {
	var svc corev1.Service
	if err := k8sclient.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, &svc); apierrors.IsNotFound(err) {
		return nil
	} else if err != nil {
		return err
	}

	return k8sclient.Delete(ctx, &svc)
}

// DeleteAutoscaler as version type delete response autoscaler.
func DeleteAutoscaler(ctx context.Context, k8sclient client.Client, namespace, name string, autoscalerVersion dorisv1.AutoScalerVersion) error {
	var autoscaler client.Object
	switch autoscalerVersion {
	case dorisv1.AutoScalerV1:
		autoscaler = &v1.HorizontalPodAutoscaler{}
	case dorisv1.AutoSclaerV2:
		autoscaler = &v2.HorizontalPodAutoscaler{}

	default:
		return errors.New(fmt.Sprintf("the autoscaler type %s is not supported.", autoscalerVersion))
	}

	if err := k8sclient.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, autoscaler); apierrors.IsNotFound(err) {
		return nil
	} else if err != nil {
		return err
	}

	return k8sclient.Delete(ctx, autoscaler)
}

func PodIsReady(status *corev1.PodStatus) bool {
	if status.ContainerStatuses == nil {
		return false
	}

	for _, cs := range status.ContainerStatuses {
		if !cs.Ready {
			return false
		}
	}

	return true
}

// GetConfigMap get the configmap name=name, namespace=namespace.
func GetConfigMap(ctx context.Context, k8scient client.Client, namespace, name string) (*corev1.ConfigMap, error) {
	var configMap corev1.ConfigMap
	if err := k8scient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, &configMap); err != nil {
		return nil, err
	}

	return &configMap, nil
}

// DeletePersistentVolumeClaim delete all PersistentVolumeClaim.
func DeletePersistentVolumeClaims(ctx context.Context, k8sclient client.Client, dcr *dorisv1.DorisCluster, componentType dorisv1.ComponentType) error {
	selector := dorisv1.GenerateStatefulSetSelector(dcr, componentType)
	return k8sclient.DeleteAllOf(ctx, &corev1.PersistentVolumeClaim{}, client.InNamespace(dcr.Namespace), client.MatchingLabels(selector))
}

func AddFinalizers(ctx context.Context, k8sclient client.Client, dcr *dorisv1.DorisCluster) error {
	for _, finalizer := range dcr.Finalizers {
		if finalizer == dorisv1.DorisFinalizer {
			return nil
		}
	}

	dcr.Finalizers = append(dcr.Finalizers, dorisv1.DorisFinalizer)
	return PatchClientObject(ctx, k8sclient, dcr)
}

func RemoveFinalizers(ctx context.Context, k8sclient client.Client, dcr *dorisv1.DorisCluster) error {
	currentFinalizers := []string{}
	for _, finalizer := range dcr.Finalizers {
		if finalizer == dorisv1.DorisFinalizer {
			continue
		}
		currentFinalizers = append(currentFinalizers, finalizer)
	}

	if len(dcr.Finalizers) > 0 {
		dcr.Finalizers = currentFinalizers
		UpdateClientObject(ctx, k8sclient, dcr)
	}

	return nil
}
