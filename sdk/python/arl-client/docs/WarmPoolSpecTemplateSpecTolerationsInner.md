# WarmPoolSpecTemplateSpecTolerationsInner


## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**effect** | **str** |  | [optional] 
**key** | **str** |  | [optional] 
**operator** | **str** |  | [optional] 
**toleration_seconds** | **int** |  | [optional] 
**value** | **str** |  | [optional] 

## Example

```python
from arl_client.models.warm_pool_spec_template_spec_tolerations_inner import WarmPoolSpecTemplateSpecTolerationsInner

# TODO update the JSON string below
json = "{}"
# create an instance of WarmPoolSpecTemplateSpecTolerationsInner from a JSON string
warm_pool_spec_template_spec_tolerations_inner_instance = WarmPoolSpecTemplateSpecTolerationsInner.from_json(json)
# print the JSON string representation of the object
print(WarmPoolSpecTemplateSpecTolerationsInner.to_json())

# convert the object into a dict
warm_pool_spec_template_spec_tolerations_inner_dict = warm_pool_spec_template_spec_tolerations_inner_instance.to_dict()
# create an instance of WarmPoolSpecTemplateSpecTolerationsInner from a dict
warm_pool_spec_template_spec_tolerations_inner_from_dict = WarmPoolSpecTemplateSpecTolerationsInner.from_dict(warm_pool_spec_template_spec_tolerations_inner_dict)
```
[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


