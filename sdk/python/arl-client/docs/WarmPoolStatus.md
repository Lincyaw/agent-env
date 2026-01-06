# WarmPoolStatus


## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**ready_replicas** | **int** |  | [optional] 
**allocated_replicas** | **int** |  | [optional] 
**conditions** | [**List[Condition]**](Condition.md) |  | [optional] 

## Example

```python
from arl_client.models.warm_pool_status import WarmPoolStatus

# TODO update the JSON string below
json = "{}"
# create an instance of WarmPoolStatus from a JSON string
warm_pool_status_instance = WarmPoolStatus.from_json(json)
# print the JSON string representation of the object
print(WarmPoolStatus.to_json())

# convert the object into a dict
warm_pool_status_dict = warm_pool_status_instance.to_dict()
# create an instance of WarmPoolStatus from a dict
warm_pool_status_from_dict = WarmPoolStatus.from_dict(warm_pool_status_dict)
```
[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


