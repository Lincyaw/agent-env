# WarmPoolSpecTemplateSpecVolumesInnerVsphereVolume


## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**fs_type** | **str** |  | [optional] 
**storage_policy_id** | **str** |  | [optional] 
**storage_policy_name** | **str** |  | [optional] 
**volume_path** | **str** |  | 

## Example

```python
from arl_client.models.warm_pool_spec_template_spec_volumes_inner_vsphere_volume import WarmPoolSpecTemplateSpecVolumesInnerVsphereVolume

# TODO update the JSON string below
json = "{}"
# create an instance of WarmPoolSpecTemplateSpecVolumesInnerVsphereVolume from a JSON string
warm_pool_spec_template_spec_volumes_inner_vsphere_volume_instance = WarmPoolSpecTemplateSpecVolumesInnerVsphereVolume.from_json(json)
# print the JSON string representation of the object
print(WarmPoolSpecTemplateSpecVolumesInnerVsphereVolume.to_json())

# convert the object into a dict
warm_pool_spec_template_spec_volumes_inner_vsphere_volume_dict = warm_pool_spec_template_spec_volumes_inner_vsphere_volume_instance.to_dict()
# create an instance of WarmPoolSpecTemplateSpecVolumesInnerVsphereVolume from a dict
warm_pool_spec_template_spec_volumes_inner_vsphere_volume_from_dict = WarmPoolSpecTemplateSpecVolumesInnerVsphereVolume.from_dict(warm_pool_spec_template_spec_volumes_inner_vsphere_volume_dict)
```
[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


