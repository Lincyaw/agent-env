# WarmPoolSpecTemplateSpecContainersInnerEnvInnerValueFrom


## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**config_map_key_ref** | [**WarmPoolSpecTemplateSpecContainersInnerEnvInnerValueFromConfigMapKeyRef**](WarmPoolSpecTemplateSpecContainersInnerEnvInnerValueFromConfigMapKeyRef.md) |  | [optional] 
**field_ref** | [**WarmPoolSpecTemplateSpecContainersInnerEnvInnerValueFromFieldRef**](WarmPoolSpecTemplateSpecContainersInnerEnvInnerValueFromFieldRef.md) |  | [optional] 
**file_key_ref** | [**WarmPoolSpecTemplateSpecContainersInnerEnvInnerValueFromFileKeyRef**](WarmPoolSpecTemplateSpecContainersInnerEnvInnerValueFromFileKeyRef.md) |  | [optional] 
**resource_field_ref** | [**WarmPoolSpecTemplateSpecContainersInnerEnvInnerValueFromResourceFieldRef**](WarmPoolSpecTemplateSpecContainersInnerEnvInnerValueFromResourceFieldRef.md) |  | [optional] 
**secret_key_ref** | [**WarmPoolSpecTemplateSpecContainersInnerEnvInnerValueFromConfigMapKeyRef**](WarmPoolSpecTemplateSpecContainersInnerEnvInnerValueFromConfigMapKeyRef.md) |  | [optional] 

## Example

```python
from arl_client.models.warm_pool_spec_template_spec_containers_inner_env_inner_value_from import WarmPoolSpecTemplateSpecContainersInnerEnvInnerValueFrom

# TODO update the JSON string below
json = "{}"
# create an instance of WarmPoolSpecTemplateSpecContainersInnerEnvInnerValueFrom from a JSON string
warm_pool_spec_template_spec_containers_inner_env_inner_value_from_instance = WarmPoolSpecTemplateSpecContainersInnerEnvInnerValueFrom.from_json(json)
# print the JSON string representation of the object
print(WarmPoolSpecTemplateSpecContainersInnerEnvInnerValueFrom.to_json())

# convert the object into a dict
warm_pool_spec_template_spec_containers_inner_env_inner_value_from_dict = warm_pool_spec_template_spec_containers_inner_env_inner_value_from_instance.to_dict()
# create an instance of WarmPoolSpecTemplateSpecContainersInnerEnvInnerValueFrom from a dict
warm_pool_spec_template_spec_containers_inner_env_inner_value_from_from_dict = WarmPoolSpecTemplateSpecContainersInnerEnvInnerValueFrom.from_dict(warm_pool_spec_template_spec_containers_inner_env_inner_value_from_dict)
```
[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


