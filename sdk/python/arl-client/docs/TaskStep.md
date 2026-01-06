# TaskStep

Task step definition. Each step can be either:  1. **FilePatch**: Create or modify a file    - Required: name, type=\"FilePatch\", path, content    - Example: {\"name\": \"write\", \"type\": \"FilePatch\", \"path\": \"/workspace/test.py\", \"content\": \"print('test')\"}  2. **Command**: Execute a command    - Required: name, type=\"Command\", command    - Optional: workDir, env    - Example: {\"name\": \"run\", \"type\": \"Command\", \"command\": [\"python\", \"test.py\"], \"env\": {\"DEBUG\": \"1\"}} 

## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**command** | **List[str]** | Command and arguments to execute (required for Command steps, ignored for FilePatch steps) | [optional] 
**content** | **str** | File content to write (required for FilePatch steps, ignored for Command steps) | [optional] 
**env** | **Dict[str, str]** | Environment variables as key-value pairs (optional, only for Command steps) | [optional] 
**name** | **str** | Step identifier (unique within the task) | 
**path** | **str** | File path to create or modify (required for FilePatch steps, ignored for Command steps) | [optional] 
**type** | **str** | Step type - either &#39;FilePatch&#39; (create/modify files) or &#39;Command&#39; (execute commands) | 
**work_dir** | **str** | Working directory for command execution (optional, only for Command steps) | [optional] 

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


