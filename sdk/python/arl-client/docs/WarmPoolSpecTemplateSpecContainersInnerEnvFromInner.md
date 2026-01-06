# WarmPoolSpecTemplateSpecContainersInnerEnvFromInner


## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**config_map_ref** | [**WarmPoolSpecTemplateSpecContainersInnerEnvFromInnerConfigMapRef**](WarmPoolSpecTemplateSpecContainersInnerEnvFromInnerConfigMapRef.md) |  | [optional] 
**prefix** | **str** |  | [optional] 
**secret_ref** | [**WarmPoolSpecTemplateSpecContainersInnerEnvFromInnerConfigMapRef**](WarmPoolSpecTemplateSpecContainersInnerEnvFromInnerConfigMapRef.md) |  | [optional] 

## Example

```python
from arl_client.models.warm_pool_spec_template_spec_containers_inner_env_from_inner import WarmPoolSpecTemplateSpecContainersInnerEnvFromInner

# TODO update the JSON string below
json = "{}"
# create an instance of WarmPoolSpecTemplateSpecContainersInnerEnvFromInner from a JSON string
warm_pool_spec_template_spec_containers_inner_env_from_inner_instance = WarmPoolSpecTemplateSpecContainersInnerEnvFromInner.from_json(json)
# print the JSON string representation of the object
print(WarmPoolSpecTemplateSpecContainersInnerEnvFromInner.to_json())

# convert the object into a dict
warm_pool_spec_template_spec_containers_inner_env_from_inner_dict = warm_pool_spec_template_spec_containers_inner_env_from_inner_instance.to_dict()
# create an instance of WarmPoolSpecTemplateSpecContainersInnerEnvFromInner from a dict
warm_pool_spec_template_spec_containers_inner_env_from_inner_from_dict = WarmPoolSpecTemplateSpecContainersInnerEnvFromInner.from_dict(warm_pool_spec_template_spec_containers_inner_env_from_inner_dict)
```
[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


