# WarmPoolSpecTemplateSpecContainersInnerEnvInner


## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**name** | **str** |  | 
**value** | **str** |  | [optional] 
**value_from** | [**WarmPoolSpecTemplateSpecContainersInnerEnvInnerValueFrom**](WarmPoolSpecTemplateSpecContainersInnerEnvInnerValueFrom.md) |  | [optional] 

## Example

```python
from arl_client.models.warm_pool_spec_template_spec_containers_inner_env_inner import WarmPoolSpecTemplateSpecContainersInnerEnvInner

# TODO update the JSON string below
json = "{}"
# create an instance of WarmPoolSpecTemplateSpecContainersInnerEnvInner from a JSON string
warm_pool_spec_template_spec_containers_inner_env_inner_instance = WarmPoolSpecTemplateSpecContainersInnerEnvInner.from_json(json)
# print the JSON string representation of the object
print(WarmPoolSpecTemplateSpecContainersInnerEnvInner.to_json())

# convert the object into a dict
warm_pool_spec_template_spec_containers_inner_env_inner_dict = warm_pool_spec_template_spec_containers_inner_env_inner_instance.to_dict()
# create an instance of WarmPoolSpecTemplateSpecContainersInnerEnvInner from a dict
warm_pool_spec_template_spec_containers_inner_env_inner_from_dict = WarmPoolSpecTemplateSpecContainersInnerEnvInner.from_dict(warm_pool_spec_template_spec_containers_inner_env_inner_dict)
```
[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


