# WarmPoolSpecTemplateSpecContainersInnerLifecyclePostStart


## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**var_exec** | [**WarmPoolSpecTemplateSpecContainersInnerLifecyclePostStartExec**](WarmPoolSpecTemplateSpecContainersInnerLifecyclePostStartExec.md) |  | [optional] 
**http_get** | [**WarmPoolSpecTemplateSpecContainersInnerLifecyclePostStartHttpGet**](WarmPoolSpecTemplateSpecContainersInnerLifecyclePostStartHttpGet.md) |  | [optional] 
**sleep** | [**WarmPoolSpecTemplateSpecContainersInnerLifecyclePostStartSleep**](WarmPoolSpecTemplateSpecContainersInnerLifecyclePostStartSleep.md) |  | [optional] 
**tcp_socket** | [**WarmPoolSpecTemplateSpecContainersInnerLifecyclePostStartTcpSocket**](WarmPoolSpecTemplateSpecContainersInnerLifecyclePostStartTcpSocket.md) |  | [optional] 

## Example

```python
from arl_client.models.warm_pool_spec_template_spec_containers_inner_lifecycle_post_start import WarmPoolSpecTemplateSpecContainersInnerLifecyclePostStart

# TODO update the JSON string below
json = "{}"
# create an instance of WarmPoolSpecTemplateSpecContainersInnerLifecyclePostStart from a JSON string
warm_pool_spec_template_spec_containers_inner_lifecycle_post_start_instance = WarmPoolSpecTemplateSpecContainersInnerLifecyclePostStart.from_json(json)
# print the JSON string representation of the object
print(WarmPoolSpecTemplateSpecContainersInnerLifecyclePostStart.to_json())

# convert the object into a dict
warm_pool_spec_template_spec_containers_inner_lifecycle_post_start_dict = warm_pool_spec_template_spec_containers_inner_lifecycle_post_start_instance.to_dict()
# create an instance of WarmPoolSpecTemplateSpecContainersInnerLifecyclePostStart from a dict
warm_pool_spec_template_spec_containers_inner_lifecycle_post_start_from_dict = WarmPoolSpecTemplateSpecContainersInnerLifecyclePostStart.from_dict(warm_pool_spec_template_spec_containers_inner_lifecycle_post_start_dict)
```
[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


