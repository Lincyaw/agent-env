# SandboxSpec


## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**keep_alive** | **bool** |  | [optional] 
**pool_ref** | **str** |  | 
**resources** | [**SandboxSpecResources**](SandboxSpecResources.md) |  | [optional] 

## Example

```python
from arl_client.models.sandbox_spec import SandboxSpec

# TODO update the JSON string below
json = "{}"
# create an instance of SandboxSpec from a JSON string
sandbox_spec_instance = SandboxSpec.from_json(json)
# print the JSON string representation of the object
print(SandboxSpec.to_json())

# convert the object into a dict
sandbox_spec_dict = sandbox_spec_instance.to_dict()
# create an instance of SandboxSpec from a dict
sandbox_spec_from_dict = SandboxSpec.from_dict(sandbox_spec_dict)
```
[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


