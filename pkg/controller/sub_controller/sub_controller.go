package sub_controller

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	dorisv1 "github.com/selectdb/doris-operator/api/doris/v1"
	"github.com/selectdb/doris-operator/pkg/common/utils/k8s"
	"github.com/selectdb/doris-operator/pkg/common/utils/resource"
	appv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type SubController interface {
	//Sync reconcile for sub controller. bool represent the component have updated.
	Sync(ctx context.Context, cluster *dorisv1.DorisCluster) error
	//clear all resource about sub-component.
	ClearResources(ctx context.Context, cluster *dorisv1.DorisCluster) (bool, error)

	//return the controller name, beController, feController,cnController for log.
	GetControllerName() string

	//UpdateStatus update the component status on src.
	UpdateComponentStatus(cluster *dorisv1.DorisCluster) error
}

// SubDefaultController define common function for all component about doris.
type SubDefaultController struct {
	K8sclient   client.Client
	K8srecorder record.EventRecorder
}

// UpdateStatus update the component status on src.
func (d *SubDefaultController) UpdateStatus(namespace string, status *dorisv1.ComponentStatus, labels map[string]string, replicas int32) error {
	return d.ClassifyPodsByStatus(namespace, status, labels, replicas)
}

func (d *SubDefaultController) ClassifyPodsByStatus(namespace string, status *dorisv1.ComponentStatus, labels map[string]string, replicas int32) error {
	var podList corev1.PodList
	if err := d.K8sclient.List(context.Background(), &podList, client.InNamespace(namespace), client.MatchingLabels(labels)); err != nil {
		return err
	}

	var creatings, readys, faileds []string
	podmap := make(map[string]corev1.Pod)
	//get all pod status that controlled by st.
	for _, pod := range podList.Items {
		podmap[pod.Name] = pod
		if ready := k8s.PodIsReady(&pod.Status); ready {
			readys = append(readys, pod.Name)
		} else if pod.Status.Phase == corev1.PodRunning || pod.Status.Phase == corev1.PodPending {
			creatings = append(creatings, pod.Name)
		} else {
			faileds = append(faileds, pod.Name)
		}
	}

	status.ComponentCondition.Phase = dorisv1.Reconciling
	if len(readys) == int(replicas) {
		status.ComponentCondition.Phase = dorisv1.Available
	} else if len(faileds) != 0 {
		status.ComponentCondition.Phase = dorisv1.HaveMemberFailed
		status.ComponentCondition.Reason = podmap[faileds[0]].Status.Reason
		status.ComponentCondition.Message = podmap[faileds[0]].Status.Message
	} else if len(creatings) != 0 {
		status.ComponentCondition.Reason = podmap[creatings[0]].Status.Reason
		status.ComponentCondition.Message = podmap[creatings[0]].Status.Message
	}

	status.RunningMembers = readys
	status.FailedMembers = faileds
	status.CreatingMembers = creatings
	return nil
}

func (d *SubDefaultController) GetConfig(ctx context.Context, configMapInfo *dorisv1.ConfigMapInfo, namespace string) (map[string]interface{}, error) {
	if configMapInfo.ConfigMapName == "" {
		return make(map[string]interface{}), nil
	}

	configMap, err := k8s.GetConfigMap(ctx, d.K8sclient, namespace, configMapInfo.ConfigMapName)
	if err != nil && apierrors.IsNotFound(err) {
		klog.Info("SubDefaultController GetCnConfig config is not exist namespace ", namespace, " configmapName ", configMapInfo.ConfigMapName)
		return make(map[string]interface{}), nil
	} else if err != nil {
		return make(map[string]interface{}), err
	}

	res, err := resource.ResolveConfigMap(configMap, configMapInfo.ResolveKey)
	return res, err
}

// ClearCommonResources clear common resources all component have, as statefulset, service.
// response `bool` represents all resource have deleted, if not and delete resource failed return false for next reconcile retry.
func (d *SubDefaultController) ClearCommonResources(ctx context.Context, dcr *dorisv1.DorisCluster, componentType dorisv1.ComponentType) (bool, error) {
	//if the doris is not have cn.
	stName := dorisv1.GenerateComponentStatefulSetName(dcr, componentType)
	externalServiceName := dorisv1.GenerateExternalServiceName(dcr, componentType)
	internalServiceName := dorisv1.GenerateInternalCommunicateServiceName(dcr, componentType)
	if err := k8s.DeleteStatefulset(ctx, d.K8sclient, dcr.Namespace, stName); err != nil && !apierrors.IsNotFound(err) {
		klog.Errorf("SubDefaultController ClearResources delete statefulset failed, namespace=%s,name=%s, error=%s.", dcr.Namespace, stName, err.Error())
		return false, err
	}

	if err := k8s.DeleteService(ctx, d.K8sclient, dcr.Namespace, internalServiceName); err != nil && !apierrors.IsNotFound(err) {
		klog.Errorf("SubDefaultController ClearResources delete search service, namespace=%s,name=%s,error=%s.", dcr.Namespace, internalServiceName, err.Error())
		return false, err
	}
	if err := k8s.DeleteService(ctx, d.K8sclient, dcr.Namespace, externalServiceName); err != nil && !apierrors.IsNotFound(err) {
		klog.Errorf("SubDefaultController ClearResources delete external service, namespace=%s, name=%s,error=%s.", dcr.Namespace, externalServiceName, err.Error())
		return false, err
	}
	if err := k8s.DeletePersistentVolumeClaims(ctx, d.K8sclient, dcr, componentType); err != nil {
		klog.Errorf("SubDefaultController ClearResources delete all PersistentVolumeClaim, namespace=%s, name=%s,error=%s.", dcr.Namespace, dcr.Name, err.Error())
		return false, err
	}

	return true, nil
}

func (d *SubDefaultController) FeAvailable(dcr *dorisv1.DorisCluster) bool {
	addr, _ := dorisv1.GetConfigFEAddrForAccess(dcr, dorisv1.Component_BE)
	if addr != "" {
		return true
	}

	//if fe deploy in k8s, should wait fe available
	//1. wait for fe ok.
	endpoints := corev1.Endpoints{}
	if err := d.K8sclient.Get(context.Background(), types.NamespacedName{Namespace: dcr.Namespace, Name: dorisv1.GenerateExternalServiceName(dcr, dorisv1.Component_FE)}, &endpoints); err != nil {
		klog.Infof("SubDefaultController Sync wait fe service name %s available occur failed %s\n", dorisv1.GenerateExternalServiceName(dcr, dorisv1.Component_FE), err.Error())
		return false
	}

	for _, sub := range endpoints.Subsets {
		if len(sub.Addresses) > 0 {
			return true
		}
	}
	return false
}

func (d *SubDefaultController) RestrictConditionsEqual(nst *appv1.StatefulSet, est *appv1.StatefulSet) {
	//shield persistent volume update when the pvcProvider=Operator
	//in webhook should intercept the volume spec updated when use statefulset pvc.
	nst.Spec.VolumeClaimTemplates = est.Spec.VolumeClaimTemplates
}

// PrepareReconcileResources prepare resource for reconcile
// response: bool, if true presents resource have ready for reconciling, if false presents resource is preparing.
func (d *SubDefaultController) PrepareReconcileResources(ctx context.Context, dcr *dorisv1.DorisCluster, componentType dorisv1.ComponentType) bool {
	switch componentType {
	case dorisv1.Component_FE:
		return d.prepareFEReconcileResources(ctx, dcr)
	case dorisv1.Component_BE:
		return d.prepareBEReconcileResources(ctx, dcr)
	case dorisv1.Component_CN:
		return d.prepareCNReconcileResources(ctx, dcr)
	default:
		klog.Infof("prepareReconcileResource not support type=", componentType)
		return true
	}
}

// prepareFEReconcileResources prepare resource for fe reconcile
// response: bool, if true presents resource have ready for fe reconciling, if false presents resource is preparing.
func (d *SubDefaultController) prepareFEReconcileResources(ctx context.Context, dcr *dorisv1.DorisCluster) bool {
	if len(dcr.Spec.FeSpec.PersistentVolumes) != 0 {
		return d.preparePersistentVolumeClaim(ctx, dcr, dorisv1.Component_FE)
	}

	return true
}

// prepareBEReconcileResources prepare resource for be reconcile
// response: bool, if true presents resource have ready for be reconciling, if false presents resource is preparing.
func (d *SubDefaultController) prepareBEReconcileResources(ctx context.Context, dcr *dorisv1.DorisCluster) bool {
	if len(dcr.Spec.BeSpec.PersistentVolumes) != 0 {
		return d.preparePersistentVolumeClaim(ctx, dcr, dorisv1.Component_BE)
	}

	return true
}

// prepareCNReconcileResources prepare resource for cn reconcile
// response: bool, if true presents resource have ready for cn reconciling, if false presents resource is preparing.
func (d *SubDefaultController) prepareCNReconcileResources(ctx context.Context, dcr *dorisv1.DorisCluster) bool {
	if len(dcr.Spec.CnSpec.PersistentVolumes) != 0 {
		return d.preparePersistentVolumeClaim(ctx, dcr, dorisv1.Component_CN)
	}

	return true
}

// 1. list pvcs, create or update,
// 1.1 labels use statefulset selector.
// 2. classify pvcs by dorisv1.PersistentVolume.name
// 2.1 travel pvcs, use key="-^"+volume.name, value=pvc put into map. starting with "-^" as the k8s resource name not allowed start with it.
func (d *SubDefaultController) preparePersistentVolumeClaim(ctx context.Context, dcr *dorisv1.DorisCluster, componentType dorisv1.ComponentType) bool {
	var volumes []dorisv1.PersistentVolume
	var replicas int32
	switch componentType {
	case dorisv1.Component_FE:
		volumes = dcr.Spec.FeSpec.PersistentVolumes
		replicas = *dcr.Spec.FeSpec.Replicas
	case dorisv1.Component_BE:
		volumes = dcr.Spec.BeSpec.PersistentVolumes
		replicas = *dcr.Spec.BeSpec.Replicas
	case dorisv1.Component_CN:
		volumes = dcr.Spec.CnSpec.PersistentVolumes
		replicas = *dcr.Spec.CnSpec.Replicas
	default:
	}

	pvcList := corev1.PersistentVolumeClaimList{}
	selector := dorisv1.GenerateStatefulSetSelector(dcr, componentType)
	stsName := dorisv1.GenerateComponentStatefulSetName(dcr, componentType)
	if err := d.K8sclient.List(ctx, &pvcList, client.InNamespace(dcr.Namespace), client.MatchingLabels(selector)); err != nil {
		d.K8srecorder.Event(dcr, EventWarning, PVCListFailed, string("list component "+componentType+" failed!"))
		return false
	}
	//classify pvc by volume.Name, pvc.name generate by volume.Name + statefulset.Name + ordinal
	pvcMap := make(map[string][]corev1.PersistentVolumeClaim)

	for _, pvc := range pvcList.Items {
		//start with unique string for classify pvc, avoid empty string match all pvc.Name
		key := "-^"
		for _, volume := range volumes {
			if volume.Name != "" && strings.HasPrefix(pvc.Name, volume.Name) {
				key = key + volume.Name
				break
			}
		}

		if _, ok := pvcMap[key]; !ok {
			pvcMap[key] = []corev1.PersistentVolumeClaim{}
		}
		pvcMap[key] = append(pvcMap[key], pvc)
	}

	//presents the pvc have all created or updated to new version.
	prepared := true
	for _, volume := range volumes {
		// if provider not `operator` should not manage pvc.
		if volume.PVCProvisioner != dorisv1.PVCProvisionerOperator {
			continue
		}

		if !d.patchPVCs(ctx, dcr, selector, pvcMap["-^"+volume.Name], stsName, volume, replicas) {
			prepared = false
		}
	}

	return prepared
}

func (d *SubDefaultController) patchPVCs(ctx context.Context, dcr *dorisv1.DorisCluster, selector map[string]string,
	pvcs []corev1.PersistentVolumeClaim, stsName string, volume dorisv1.PersistentVolume, replicas int32) bool {
	//patch already exist in k8s .
	prepared := true
	for _, pvc := range pvcs {
		oldCapacity := pvc.Spec.Resources.Requests[corev1.ResourceStorage]
		newCapacity := volume.PersistentVolumeClaimSpec.Resources.Requests[corev1.ResourceStorage]
		if !oldCapacity.Equal(newCapacity) {
			// if pvc need update, the resource have not prepared, return false.
			prepared = false
			eventType := EventNormal
			reason := PVCUpdate
			message := pvc.Name + " update successfully!"
			pvc.Spec.Resources.Requests[corev1.ResourceStorage] = newCapacity
			if err := d.K8sclient.Patch(ctx, &pvc, client.Merge); err != nil {
				klog.Errorf("SubDefaultController namespace %s name %s patch pvc %s failed, %s", dcr.Namespace, dcr.Name, pvc.Name, err.Error())
				eventType = EventWarning
				reason = PVCUpdateFailed
				message = pvc.Name + " update failed, " + err.Error()
			}

			d.K8srecorder.Event(dcr, eventType, reason, message)
		}
	}

	// if need add new pvc, the resource prepared not finished, return false.
	if len(pvcs) < int(replicas) {
		prepared = false
		d.K8srecorder.Event(dcr, EventNormal, PVCCreate, fmt.Sprintf("create PVC ordinal %d - %d", len(pvcs), replicas))
	}

	baseOrdinal := len(pvcs)
	for ; baseOrdinal < int(replicas); baseOrdinal++ {
		pvc := resource.BuildPVC(volume, selector, dcr.Namespace, stsName, strconv.Itoa(baseOrdinal))
		if err := d.K8sclient.Create(ctx, &pvc); err != nil && !apierrors.IsAlreadyExists(err) {
			d.K8srecorder.Event(dcr, EventWarning, PVCCreateFailed, err.Error())
			klog.Errorf("SubDefaultController namespace %s name %s create pvc %s failed, %s.", dcr.Namespace, dcr.Name, pvc.Name)
		}
	}

	return prepared
}
