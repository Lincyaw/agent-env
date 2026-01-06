# WarmPoolSpecTemplateSpecVolumesInnerCinder


## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**fs_type** | **str** |  | [optional] 
**read_only** | **bool** |  | [optional] 
**secret_ref** | [**WarmPoolSpecTemplateSpecImagePullSecretsInner**](WarmPoolSpecTemplateSpecImagePullSecretsInner.md) |  | [optional] 
**volume_id** | **str** |  | 

## Example

```python
from arl_client.models.warm_pool_spec_template_spec_volumes_inner_cinder import WarmPoolSpecTemplateSpecVolumesInnerCinder

# TODO update the JSON string below
json = "{}"
# create an instance of WarmPoolSpecTemplateSpecVolumesInnerCinder from a JSON string
warm_pool_spec_template_spec_volumes_inner_cinder_instance = WarmPoolSpecTemplateSpecVolumesInnerCinder.from_json(json)
# print the JSON string representation of the object
print(WarmPoolSpecTemplateSpecVolumesInnerCinder.to_json())

# convert the object into a dict
warm_pool_spec_template_spec_volumes_inner_cinder_dict = warm_pool_spec_template_spec_volumes_inner_cinder_instance.to_dict()
# create an instance of WarmPoolSpecTemplateSpecVolumesInnerCinder from a dict
warm_pool_spec_template_spec_volumes_inner_cinder_from_dict = WarmPoolSpecTemplateSpecVolumesInnerCinder.from_dict(warm_pool_spec_template_spec_volumes_inner_cinder_dict)
```
[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


