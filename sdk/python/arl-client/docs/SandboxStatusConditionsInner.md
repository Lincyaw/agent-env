# SandboxStatusConditionsInner


## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**last_transition_time** | **datetime** |  | 
**message** | **str** |  | 
**observed_generation** | **int** |  | [optional] 
**reason** | **str** |  | 
**status** | **str** |  | 
**type** | **str** |  | 

## Example

```python
from arl_client.models.sandbox_status_conditions_inner import SandboxStatusConditionsInner

# TODO update the JSON string below
json = "{}"
# create an instance of SandboxStatusConditionsInner from a JSON string
sandbox_status_conditions_inner_instance = SandboxStatusConditionsInner.from_json(json)
# print the JSON string representation of the object
print(SandboxStatusConditionsInner.to_json())

# convert the object into a dict
sandbox_status_conditions_inner_dict = sandbox_status_conditions_inner_instance.to_dict()
# create an instance of SandboxStatusConditionsInner from a dict
sandbox_status_conditions_inner_from_dict = SandboxStatusConditionsInner.from_dict(sandbox_status_conditions_inner_dict)
```
[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


