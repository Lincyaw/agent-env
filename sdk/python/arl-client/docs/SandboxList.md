# SandboxList


## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**api_version** | **str** |  | [optional] 
**kind** | **str** |  | [optional] 
**metadata** | [**ListMeta**](ListMeta.md) |  | [optional] 
**items** | [**List[Sandbox]**](Sandbox.md) |  | [optional] 

## Example

```python
from arl_client.models.sandbox_list import SandboxList

# TODO update the JSON string below
json = "{}"
# create an instance of SandboxList from a JSON string
sandbox_list_instance = SandboxList.from_json(json)
# print the JSON string representation of the object
print(SandboxList.to_json())

# convert the object into a dict
sandbox_list_dict = sandbox_list_instance.to_dict()
# create an instance of SandboxList from a dict
sandbox_list_from_dict = SandboxList.from_dict(sandbox_list_dict)
```
[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


