# WarmPoolSpecTemplateSpecContainersInnerLifecycle


## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**post_start** | [**WarmPoolSpecTemplateSpecContainersInnerLifecyclePostStart**](WarmPoolSpecTemplateSpecContainersInnerLifecyclePostStart.md) |  | [optional] 
**pre_stop** | [**WarmPoolSpecTemplateSpecContainersInnerLifecyclePostStart**](WarmPoolSpecTemplateSpecContainersInnerLifecyclePostStart.md) |  | [optional] 
**stop_signal** | **str** |  | [optional] 

## Example

```python
from arl_client.models.warm_pool_spec_template_spec_containers_inner_lifecycle import WarmPoolSpecTemplateSpecContainersInnerLifecycle

# TODO update the JSON string below
json = "{}"
# create an instance of WarmPoolSpecTemplateSpecContainersInnerLifecycle from a JSON string
warm_pool_spec_template_spec_containers_inner_lifecycle_instance = WarmPoolSpecTemplateSpecContainersInnerLifecycle.from_json(json)
# print the JSON string representation of the object
print(WarmPoolSpecTemplateSpecContainersInnerLifecycle.to_json())

# convert the object into a dict
warm_pool_spec_template_spec_containers_inner_lifecycle_dict = warm_pool_spec_template_spec_containers_inner_lifecycle_instance.to_dict()
# create an instance of WarmPoolSpecTemplateSpecContainersInnerLifecycle from a dict
warm_pool_spec_template_spec_containers_inner_lifecycle_from_dict = WarmPoolSpecTemplateSpecContainersInnerLifecycle.from_dict(warm_pool_spec_template_spec_containers_inner_lifecycle_dict)
```
[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


