# SandboxSpecResources


## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**claims** | [**List[SandboxSpecResourcesClaimsInner]**](SandboxSpecResourcesClaimsInner.md) |  | [optional] 
**limits** | [**Dict[str, SandboxSpecResourcesLimitsValue]**](SandboxSpecResourcesLimitsValue.md) |  | [optional] 
**requests** | [**Dict[str, SandboxSpecResourcesLimitsValue]**](SandboxSpecResourcesLimitsValue.md) |  | [optional] 

## Example

```python
from arl_client.models.sandbox_spec_resources import SandboxSpecResources

# TODO update the JSON string below
json = "{}"
# create an instance of SandboxSpecResources from a JSON string
sandbox_spec_resources_instance = SandboxSpecResources.from_json(json)
# print the JSON string representation of the object
print(SandboxSpecResources.to_json())

# convert the object into a dict
sandbox_spec_resources_dict = sandbox_spec_resources_instance.to_dict()
# create an instance of SandboxSpecResources from a dict
sandbox_spec_resources_from_dict = SandboxSpecResources.from_dict(sandbox_spec_resources_dict)
```
[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


