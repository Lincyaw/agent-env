# TaskSpec


## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**retries** | **int** |  | [optional] 
**sandbox_ref** | **str** |  | 
**steps** | [**List[TaskStep]**](TaskStep.md) |  | 
**timeout** | **str** |  | [optional] 
**ttl_seconds_after_finished** | **int** |  | [optional] 

## Example

```python
from arl_client.models.task_spec import TaskSpec

# TODO update the JSON string below
json = "{}"
# create an instance of TaskSpec from a JSON string
task_spec_instance = TaskSpec.from_json(json)
# print the JSON string representation of the object
print(TaskSpec.to_json())

# convert the object into a dict
task_spec_dict = task_spec_instance.to_dict()
# create an instance of TaskSpec from a dict
task_spec_from_dict = TaskSpec.from_dict(task_spec_dict)
```
[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


