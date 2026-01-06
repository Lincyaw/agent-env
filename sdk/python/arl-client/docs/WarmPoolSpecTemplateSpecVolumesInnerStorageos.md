# WarmPoolSpecTemplateSpecVolumesInnerStorageos


## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**fs_type** | **str** |  | [optional] 
**read_only** | **bool** |  | [optional] 
**secret_ref** | [**WarmPoolSpecTemplateSpecImagePullSecretsInner**](WarmPoolSpecTemplateSpecImagePullSecretsInner.md) |  | [optional] 
**volume_name** | **str** |  | [optional] 
**volume_namespace** | **str** |  | [optional] 

## Example

```python
from arl_client.models.warm_pool_spec_template_spec_volumes_inner_storageos import WarmPoolSpecTemplateSpecVolumesInnerStorageos

# TODO update the JSON string below
json = "{}"
# create an instance of WarmPoolSpecTemplateSpecVolumesInnerStorageos from a JSON string
warm_pool_spec_template_spec_volumes_inner_storageos_instance = WarmPoolSpecTemplateSpecVolumesInnerStorageos.from_json(json)
# print the JSON string representation of the object
print(WarmPoolSpecTemplateSpecVolumesInnerStorageos.to_json())

# convert the object into a dict
warm_pool_spec_template_spec_volumes_inner_storageos_dict = warm_pool_spec_template_spec_volumes_inner_storageos_instance.to_dict()
# create an instance of WarmPoolSpecTemplateSpecVolumesInnerStorageos from a dict
warm_pool_spec_template_spec_volumes_inner_storageos_from_dict = WarmPoolSpecTemplateSpecVolumesInnerStorageos.from_dict(warm_pool_spec_template_spec_volumes_inner_storageos_dict)
```
[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


