# WarmPoolList


## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**api_version** | **str** |  | [optional] 
**kind** | **str** |  | [optional] 
**metadata** | [**ListMeta**](ListMeta.md) |  | [optional] 
**items** | [**List[WarmPool]**](WarmPool.md) |  | [optional] 

## Example

```python
from arl_client.models.warm_pool_list import WarmPoolList

# TODO update the JSON string below
json = "{}"
# create an instance of WarmPoolList from a JSON string
warm_pool_list_instance = WarmPoolList.from_json(json)
# print the JSON string representation of the object
print(WarmPoolList.to_json())

# convert the object into a dict
warm_pool_list_dict = warm_pool_list_instance.to_dict()
# create an instance of WarmPoolList from a dict
warm_pool_list_from_dict = WarmPoolList.from_dict(warm_pool_list_dict)
```
[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


