# TaskStep


## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**command** | **List[str]** |  | [optional] 
**content** | **str** |  | [optional] 
**env** | **Dict[str, str]** |  | [optional] 
**name** | **str** |  | 
**path** | **str** |  | [optional] 
**type** | **str** |  | 
**work_dir** | **str** |  | [optional] 

## Example

```python
from arl_client.models.task_step import TaskStep

# TODO update the JSON string below
json = "{}"
# create an instance of TaskStep from a JSON string
task_step_instance = TaskStep.from_json(json)
# print the JSON string representation of the object
print(TaskStep.to_json())

# convert the object into a dict
task_step_dict = task_step_instance.to_dict()
# create an instance of TaskStep from a dict
task_step_from_dict = TaskStep.from_dict(task_step_dict)
```
[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


