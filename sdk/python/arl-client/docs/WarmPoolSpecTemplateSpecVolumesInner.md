# WarmPoolSpecTemplateSpecVolumesInner


## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**aws_elastic_block_store** | [**WarmPoolSpecTemplateSpecVolumesInnerAwsElasticBlockStore**](WarmPoolSpecTemplateSpecVolumesInnerAwsElasticBlockStore.md) |  | [optional] 
**azure_disk** | [**WarmPoolSpecTemplateSpecVolumesInnerAzureDisk**](WarmPoolSpecTemplateSpecVolumesInnerAzureDisk.md) |  | [optional] 
**azure_file** | [**WarmPoolSpecTemplateSpecVolumesInnerAzureFile**](WarmPoolSpecTemplateSpecVolumesInnerAzureFile.md) |  | [optional] 
**cephfs** | [**WarmPoolSpecTemplateSpecVolumesInnerCephfs**](WarmPoolSpecTemplateSpecVolumesInnerCephfs.md) |  | [optional] 
**cinder** | [**WarmPoolSpecTemplateSpecVolumesInnerCinder**](WarmPoolSpecTemplateSpecVolumesInnerCinder.md) |  | [optional] 
**config_map** | [**WarmPoolSpecTemplateSpecVolumesInnerConfigMap**](WarmPoolSpecTemplateSpecVolumesInnerConfigMap.md) |  | [optional] 
**csi** | [**WarmPoolSpecTemplateSpecVolumesInnerCsi**](WarmPoolSpecTemplateSpecVolumesInnerCsi.md) |  | [optional] 
**downward_api** | [**WarmPoolSpecTemplateSpecVolumesInnerDownwardAPI**](WarmPoolSpecTemplateSpecVolumesInnerDownwardAPI.md) |  | [optional] 
**empty_dir** | [**WarmPoolSpecTemplateSpecVolumesInnerEmptyDir**](WarmPoolSpecTemplateSpecVolumesInnerEmptyDir.md) |  | [optional] 
**ephemeral** | [**WarmPoolSpecTemplateSpecVolumesInnerEphemeral**](WarmPoolSpecTemplateSpecVolumesInnerEphemeral.md) |  | [optional] 
**fc** | [**WarmPoolSpecTemplateSpecVolumesInnerFc**](WarmPoolSpecTemplateSpecVolumesInnerFc.md) |  | [optional] 
**flex_volume** | [**WarmPoolSpecTemplateSpecVolumesInnerFlexVolume**](WarmPoolSpecTemplateSpecVolumesInnerFlexVolume.md) |  | [optional] 
**flocker** | [**WarmPoolSpecTemplateSpecVolumesInnerFlocker**](WarmPoolSpecTemplateSpecVolumesInnerFlocker.md) |  | [optional] 
**gce_persistent_disk** | [**WarmPoolSpecTemplateSpecVolumesInnerGcePersistentDisk**](WarmPoolSpecTemplateSpecVolumesInnerGcePersistentDisk.md) |  | [optional] 
**git_repo** | [**WarmPoolSpecTemplateSpecVolumesInnerGitRepo**](WarmPoolSpecTemplateSpecVolumesInnerGitRepo.md) |  | [optional] 
**glusterfs** | [**WarmPoolSpecTemplateSpecVolumesInnerGlusterfs**](WarmPoolSpecTemplateSpecVolumesInnerGlusterfs.md) |  | [optional] 
**host_path** | [**WarmPoolSpecTemplateSpecVolumesInnerHostPath**](WarmPoolSpecTemplateSpecVolumesInnerHostPath.md) |  | [optional] 
**image** | [**WarmPoolSpecTemplateSpecVolumesInnerImage**](WarmPoolSpecTemplateSpecVolumesInnerImage.md) |  | [optional] 
**iscsi** | [**WarmPoolSpecTemplateSpecVolumesInnerIscsi**](WarmPoolSpecTemplateSpecVolumesInnerIscsi.md) |  | [optional] 
**name** | **str** |  | 
**nfs** | [**WarmPoolSpecTemplateSpecVolumesInnerNfs**](WarmPoolSpecTemplateSpecVolumesInnerNfs.md) |  | [optional] 
**persistent_volume_claim** | [**WarmPoolSpecTemplateSpecVolumesInnerPersistentVolumeClaim**](WarmPoolSpecTemplateSpecVolumesInnerPersistentVolumeClaim.md) |  | [optional] 
**photon_persistent_disk** | [**WarmPoolSpecTemplateSpecVolumesInnerPhotonPersistentDisk**](WarmPoolSpecTemplateSpecVolumesInnerPhotonPersistentDisk.md) |  | [optional] 
**portworx_volume** | [**WarmPoolSpecTemplateSpecVolumesInnerPortworxVolume**](WarmPoolSpecTemplateSpecVolumesInnerPortworxVolume.md) |  | [optional] 
**projected** | [**WarmPoolSpecTemplateSpecVolumesInnerProjected**](WarmPoolSpecTemplateSpecVolumesInnerProjected.md) |  | [optional] 
**quobyte** | [**WarmPoolSpecTemplateSpecVolumesInnerQuobyte**](WarmPoolSpecTemplateSpecVolumesInnerQuobyte.md) |  | [optional] 
**rbd** | [**WarmPoolSpecTemplateSpecVolumesInnerRbd**](WarmPoolSpecTemplateSpecVolumesInnerRbd.md) |  | [optional] 
**scale_io** | [**WarmPoolSpecTemplateSpecVolumesInnerScaleIO**](WarmPoolSpecTemplateSpecVolumesInnerScaleIO.md) |  | [optional] 
**secret** | [**WarmPoolSpecTemplateSpecVolumesInnerSecret**](WarmPoolSpecTemplateSpecVolumesInnerSecret.md) |  | [optional] 
**storageos** | [**WarmPoolSpecTemplateSpecVolumesInnerStorageos**](WarmPoolSpecTemplateSpecVolumesInnerStorageos.md) |  | [optional] 
**vsphere_volume** | [**WarmPoolSpecTemplateSpecVolumesInnerVsphereVolume**](WarmPoolSpecTemplateSpecVolumesInnerVsphereVolume.md) |  | [optional] 

## Example

```python
from arl_client.models.warm_pool_spec_template_spec_volumes_inner import WarmPoolSpecTemplateSpecVolumesInner

# TODO update the JSON string below
json = "{}"
# create an instance of WarmPoolSpecTemplateSpecVolumesInner from a JSON string
warm_pool_spec_template_spec_volumes_inner_instance = WarmPoolSpecTemplateSpecVolumesInner.from_json(json)
# print the JSON string representation of the object
print(WarmPoolSpecTemplateSpecVolumesInner.to_json())

# convert the object into a dict
warm_pool_spec_template_spec_volumes_inner_dict = warm_pool_spec_template_spec_volumes_inner_instance.to_dict()
# create an instance of WarmPoolSpecTemplateSpecVolumesInner from a dict
warm_pool_spec_template_spec_volumes_inner_from_dict = WarmPoolSpecTemplateSpecVolumesInner.from_dict(warm_pool_spec_template_spec_volumes_inner_dict)
```
[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


