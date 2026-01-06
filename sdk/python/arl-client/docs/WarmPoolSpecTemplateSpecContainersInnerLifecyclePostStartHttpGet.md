# WarmPoolSpecTemplateSpecContainersInnerLifecyclePostStartHttpGet


## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**host** | **str** |  | [optional] 
**http_headers** | [**List[WarmPoolSpecTemplateSpecContainersInnerLifecyclePostStartHttpGetHttpHeadersInner]**](WarmPoolSpecTemplateSpecContainersInnerLifecyclePostStartHttpGetHttpHeadersInner.md) |  | [optional] 
**path** | **str** |  | [optional] 
**port** | [**WarmPoolSpecTemplateSpecContainersInnerLifecyclePostStartHttpGetPort**](WarmPoolSpecTemplateSpecContainersInnerLifecyclePostStartHttpGetPort.md) |  | 
**scheme** | **str** |  | [optional] 

## Example

```python
from arl_client.models.warm_pool_spec_template_spec_containers_inner_lifecycle_post_start_http_get import WarmPoolSpecTemplateSpecContainersInnerLifecyclePostStartHttpGet

# TODO update the JSON string below
json = "{}"
# create an instance of WarmPoolSpecTemplateSpecContainersInnerLifecyclePostStartHttpGet from a JSON string
warm_pool_spec_template_spec_containers_inner_lifecycle_post_start_http_get_instance = WarmPoolSpecTemplateSpecContainersInnerLifecyclePostStartHttpGet.from_json(json)
# print the JSON string representation of the object
print(WarmPoolSpecTemplateSpecContainersInnerLifecyclePostStartHttpGet.to_json())

# convert the object into a dict
warm_pool_spec_template_spec_containers_inner_lifecycle_post_start_http_get_dict = warm_pool_spec_template_spec_containers_inner_lifecycle_post_start_http_get_instance.to_dict()
# create an instance of WarmPoolSpecTemplateSpecContainersInnerLifecyclePostStartHttpGet from a dict
warm_pool_spec_template_spec_containers_inner_lifecycle_post_start_http_get_from_dict = WarmPoolSpecTemplateSpecContainersInnerLifecyclePostStartHttpGet.from_dict(warm_pool_spec_template_spec_containers_inner_lifecycle_post_start_http_get_dict)
```
[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


