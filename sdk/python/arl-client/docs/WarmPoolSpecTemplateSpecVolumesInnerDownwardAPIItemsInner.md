# WarmPoolSpecTemplateSpecVolumesInnerDownwardAPIItemsInner


## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**field_ref** | [**WarmPoolSpecTemplateSpecContainersInnerEnvInnerValueFromFieldRef**](WarmPoolSpecTemplateSpecContainersInnerEnvInnerValueFromFieldRef.md) |  | [optional] 
**mode** | **int** |  | [optional] 
**path** | **str** |  | 
**resource_field_ref** | [**WarmPoolSpecTemplateSpecContainersInnerEnvInnerValueFromResourceFieldRef**](WarmPoolSpecTemplateSpecContainersInnerEnvInnerValueFromResourceFieldRef.md) |  | [optional] 

## Example

```python
from arl_client.models.warm_pool_spec_template_spec_volumes_inner_downward_api_items_inner import WarmPoolSpecTemplateSpecVolumesInnerDownwardAPIItemsInner

# TODO update the JSON string below
json = "{}"
# create an instance of WarmPoolSpecTemplateSpecVolumesInnerDownwardAPIItemsInner from a JSON string
warm_pool_spec_template_spec_volumes_inner_downward_api_items_inner_instance = WarmPoolSpecTemplateSpecVolumesInnerDownwardAPIItemsInner.from_json(json)
# print the JSON string representation of the object
print(WarmPoolSpecTemplateSpecVolumesInnerDownwardAPIItemsInner.to_json())

# convert the object into a dict
warm_pool_spec_template_spec_volumes_inner_downward_api_items_inner_dict = warm_pool_spec_template_spec_volumes_inner_downward_api_items_inner_instance.to_dict()
# create an instance of WarmPoolSpecTemplateSpecVolumesInnerDownwardAPIItemsInner from a dict
warm_pool_spec_template_spec_volumes_inner_downward_api_items_inner_from_dict = WarmPoolSpecTemplateSpecVolumesInnerDownwardAPIItemsInner.from_dict(warm_pool_spec_template_spec_volumes_inner_downward_api_items_inner_dict)
```
[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


