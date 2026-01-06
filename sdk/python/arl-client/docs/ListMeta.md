# ListMeta


## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**resource_version** | **str** |  | [optional] 
**var_continue** | **str** |  | [optional] 

## Example

```python
from arl_client.models.list_meta import ListMeta

# TODO update the JSON string below
json = "{}"
# create an instance of ListMeta from a JSON string
list_meta_instance = ListMeta.from_json(json)
# print the JSON string representation of the object
print(ListMeta.to_json())

# convert the object into a dict
list_meta_dict = list_meta_instance.to_dict()
# create an instance of ListMeta from a dict
list_meta_from_dict = ListMeta.from_dict(list_meta_dict)
```
[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


