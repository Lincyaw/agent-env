# WarmPool


## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**api_version** | **str** |  | [optional] 
**kind** | **str** |  | [optional] 
**metadata** | [**ObjectMeta**](ObjectMeta.md) |  | [optional] 
**spec** | [**WarmPoolSpec**](WarmPoolSpec.md) |  | [optional] 
**status** | [**WarmPoolStatus**](WarmPoolStatus.md) |  | [optional] 

## Example

```python
from arl_client.models.warm_pool import WarmPool

# TODO update the JSON string below
json = "{}"
# create an instance of WarmPool from a JSON string
warm_pool_instance = WarmPool.from_json(json)
# print the JSON string representation of the object
print(WarmPool.to_json())

# convert the object into a dict
warm_pool_dict = warm_pool_instance.to_dict()
# create an instance of WarmPool from a dict
warm_pool_from_dict = WarmPool.from_dict(warm_pool_dict)
```
[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


