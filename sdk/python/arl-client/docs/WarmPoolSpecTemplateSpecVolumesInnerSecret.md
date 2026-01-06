# WarmPoolSpecTemplateSpecVolumesInnerSecret


## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**default_mode** | **int** |  | [optional] 
**items** | [**List[WarmPoolSpecTemplateSpecVolumesInnerConfigMapItemsInner]**](WarmPoolSpecTemplateSpecVolumesInnerConfigMapItemsInner.md) |  | [optional] 
**optional** | **bool** |  | [optional] 
**secret_name** | **str** |  | [optional] 

## Example

```python
from arl_client.models.warm_pool_spec_template_spec_volumes_inner_secret import WarmPoolSpecTemplateSpecVolumesInnerSecret

# TODO update the JSON string below
json = "{}"
# create an instance of WarmPoolSpecTemplateSpecVolumesInnerSecret from a JSON string
warm_pool_spec_template_spec_volumes_inner_secret_instance = WarmPoolSpecTemplateSpecVolumesInnerSecret.from_json(json)
# print the JSON string representation of the object
print(WarmPoolSpecTemplateSpecVolumesInnerSecret.to_json())

# convert the object into a dict
warm_pool_spec_template_spec_volumes_inner_secret_dict = warm_pool_spec_template_spec_volumes_inner_secret_instance.to_dict()
# create an instance of WarmPoolSpecTemplateSpecVolumesInnerSecret from a dict
warm_pool_spec_template_spec_volumes_inner_secret_from_dict = WarmPoolSpecTemplateSpecVolumesInnerSecret.from_dict(warm_pool_spec_template_spec_volumes_inner_secret_dict)
```
[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


