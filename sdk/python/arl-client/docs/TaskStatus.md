# TaskStatus


## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**state** | **str** |  | [optional] 
**exit_code** | **int** |  | [optional] 
**stdout** | **str** |  | [optional] 
**stderr** | **str** |  | [optional] 
**duration** | **str** |  | [optional] 
**start_time** | **datetime** |  | [optional] 
**completion_time** | **datetime** |  | [optional] 
**conditions** | [**List[Condition]**](Condition.md) |  | [optional] 

## Example

```python
from arl_client.models.task_status import TaskStatus

# TODO update the JSON string below
json = "{}"
# create an instance of TaskStatus from a JSON string
task_status_instance = TaskStatus.from_json(json)
# print the JSON string representation of the object
print(TaskStatus.to_json())

# convert the object into a dict
task_status_dict = task_status_instance.to_dict()
# create an instance of TaskStatus from a dict
task_status_from_dict = TaskStatus.from_dict(task_status_dict)
```
[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


