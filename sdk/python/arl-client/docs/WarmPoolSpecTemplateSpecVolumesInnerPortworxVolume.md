# WarmPoolSpecTemplateSpecVolumesInnerPortworxVolume


## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**fs_type** | **str** |  | [optional] 
**read_only** | **bool** |  | [optional] 
**volume_id** | **str** |  | 

## Example

```python
from arl_client.models.warm_pool_spec_template_spec_volumes_inner_portworx_volume import WarmPoolSpecTemplateSpecVolumesInnerPortworxVolume

# TODO update the JSON string below
json = "{}"
# create an instance of WarmPoolSpecTemplateSpecVolumesInnerPortworxVolume from a JSON string
warm_pool_spec_template_spec_volumes_inner_portworx_volume_instance = WarmPoolSpecTemplateSpecVolumesInnerPortworxVolume.from_json(json)
# print the JSON string representation of the object
print(WarmPoolSpecTemplateSpecVolumesInnerPortworxVolume.to_json())

# convert the object into a dict
warm_pool_spec_template_spec_volumes_inner_portworx_volume_dict = warm_pool_spec_template_spec_volumes_inner_portworx_volume_instance.to_dict()
# create an instance of WarmPoolSpecTemplateSpecVolumesInnerPortworxVolume from a dict
warm_pool_spec_template_spec_volumes_inner_portworx_volume_from_dict = WarmPoolSpecTemplateSpecVolumesInnerPortworxVolume.from_dict(warm_pool_spec_template_spec_volumes_inner_portworx_volume_dict)
```
[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


