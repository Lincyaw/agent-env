# WarmPoolSpecTemplate


## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**metadata** | **object** |  | [optional] 
**spec** | [**WarmPoolSpecTemplateSpec**](WarmPoolSpecTemplateSpec.md) |  | [optional] 

## Example

```python
from arl_client.models.warm_pool_spec_template import WarmPoolSpecTemplate

# TODO update the JSON string below
json = "{}"
# create an instance of WarmPoolSpecTemplate from a JSON string
warm_pool_spec_template_instance = WarmPoolSpecTemplate.from_json(json)
# print the JSON string representation of the object
print(WarmPoolSpecTemplate.to_json())

# convert the object into a dict
warm_pool_spec_template_dict = warm_pool_spec_template_instance.to_dict()
# create an instance of WarmPoolSpecTemplate from a dict
warm_pool_spec_template_from_dict = WarmPoolSpecTemplate.from_dict(warm_pool_spec_template_dict)
```
[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


