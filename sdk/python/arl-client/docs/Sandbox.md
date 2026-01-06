# Sandbox


## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**api_version** | **str** | APIVersion defines the versioned schema of this representation | [optional] 
**kind** | **str** | Kind is a string value representing the REST resource | [optional] 
**metadata** | [**ObjectMeta**](ObjectMeta.md) |  | [optional] 
**spec** | [**SandboxSpec**](SandboxSpec.md) |  | [optional] 
**status** | [**SandboxStatus**](SandboxStatus.md) |  | [optional] 

## Example

```python
from arl_client.models.sandbox import Sandbox

# TODO update the JSON string below
json = "{}"
# create an instance of Sandbox from a JSON string
sandbox_instance = Sandbox.from_json(json)
# print the JSON string representation of the object
print(Sandbox.to_json())

# convert the object into a dict
sandbox_dict = sandbox_instance.to_dict()
# create an instance of Sandbox from a dict
sandbox_from_dict = Sandbox.from_dict(sandbox_dict)
```
[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


