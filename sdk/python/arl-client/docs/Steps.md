# Steps


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
from arl_client.models.steps import Steps

# TODO update the JSON string below
json = "{}"
# create an instance of Steps from a JSON string
steps_instance = Steps.from_json(json)
# print the JSON string representation of the object
print(Steps.to_json())

# convert the object into a dict
steps_dict = steps_instance.to_dict()
# create an instance of Steps from a dict
steps_from_dict = Steps.from_dict(steps_dict)
```
[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


