# WarmPoolSpecTemplateSpecContainersInnerVolumeMountsInner


## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**mount_path** | **str** |  | 
**mount_propagation** | **str** |  | [optional] 
**name** | **str** |  | 
**read_only** | **bool** |  | [optional] 
**recursive_read_only** | **str** |  | [optional] 
**sub_path** | **str** |  | [optional] 
**sub_path_expr** | **str** |  | [optional] 

## Example

```python
from arl_client.models.warm_pool_spec_template_spec_containers_inner_volume_mounts_inner import WarmPoolSpecTemplateSpecContainersInnerVolumeMountsInner

# TODO update the JSON string below
json = "{}"
# create an instance of WarmPoolSpecTemplateSpecContainersInnerVolumeMountsInner from a JSON string
warm_pool_spec_template_spec_containers_inner_volume_mounts_inner_instance = WarmPoolSpecTemplateSpecContainersInnerVolumeMountsInner.from_json(json)
# print the JSON string representation of the object
print(WarmPoolSpecTemplateSpecContainersInnerVolumeMountsInner.to_json())

# convert the object into a dict
warm_pool_spec_template_spec_containers_inner_volume_mounts_inner_dict = warm_pool_spec_template_spec_containers_inner_volume_mounts_inner_instance.to_dict()
# create an instance of WarmPoolSpecTemplateSpecContainersInnerVolumeMountsInner from a dict
warm_pool_spec_template_spec_containers_inner_volume_mounts_inner_from_dict = WarmPoolSpecTemplateSpecContainersInnerVolumeMountsInner.from_dict(warm_pool_spec_template_spec_containers_inner_volume_mounts_inner_dict)
```
[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


