# SandboxStatus


## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**phase** | **str** |  | [optional] 
**pod_name** | **str** |  | [optional] 
**pod_ip** | **str** |  | [optional] 
**work_dir** | **str** |  | [optional] 
**conditions** | [**List[Condition]**](Condition.md) |  | [optional] 

## Example

```python
from arl_client.models.sandbox_status import SandboxStatus

# TODO update the JSON string below
json = "{}"
# create an instance of SandboxStatus from a JSON string
sandbox_status_instance = SandboxStatus.from_json(json)
# print the JSON string representation of the object
print(SandboxStatus.to_json())

# convert the object into a dict
sandbox_status_dict = sandbox_status_instance.to_dict()
# create an instance of SandboxStatus from a dict
sandbox_status_from_dict = SandboxStatus.from_dict(sandbox_status_dict)
```
[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


