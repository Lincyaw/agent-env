# TaskList


## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**api_version** | **str** |  | [optional] 
**kind** | **str** |  | [optional] 
**metadata** | [**ListMeta**](ListMeta.md) |  | [optional] 
**items** | [**List[Task]**](Task.md) |  | [optional] 

## Example

```python
from arl_client.models.task_list import TaskList

# TODO update the JSON string below
json = "{}"
# create an instance of TaskList from a JSON string
task_list_instance = TaskList.from_json(json)
# print the JSON string representation of the object
print(TaskList.to_json())

# convert the object into a dict
task_list_dict = task_list_instance.to_dict()
# create an instance of TaskList from a dict
task_list_from_dict = TaskList.from_dict(task_list_dict)
```
[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


