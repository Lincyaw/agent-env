# WarmPoolSpec


## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**replicas** | **int** | Number of idle pods to maintain | 
**template** | [**PodTemplateSpec**](PodTemplateSpec.md) |  | 

## Example

```python
from arl_client.models.warm_pool_spec import WarmPoolSpec

# TODO update the JSON string below
json = "{}"
# create an instance of WarmPoolSpec from a JSON string
warm_pool_spec_instance = WarmPoolSpec.from_json(json)
# print the JSON string representation of the object
print(WarmPoolSpec.to_json())

# convert the object into a dict
warm_pool_spec_dict = warm_pool_spec_instance.to_dict()
# create an instance of WarmPoolSpec from a dict
warm_pool_spec_from_dict = WarmPoolSpec.from_dict(warm_pool_spec_dict)
```
[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


