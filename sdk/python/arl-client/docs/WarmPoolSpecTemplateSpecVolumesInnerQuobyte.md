# WarmPoolSpecTemplateSpecVolumesInnerQuobyte


## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**group** | **str** |  | [optional] 
**read_only** | **bool** |  | [optional] 
**registry** | **str** |  | 
**tenant** | **str** |  | [optional] 
**user** | **str** |  | [optional] 
**volume** | **str** |  | 

## Example

```python
from arl_client.models.warm_pool_spec_template_spec_volumes_inner_quobyte import WarmPoolSpecTemplateSpecVolumesInnerQuobyte

# TODO update the JSON string below
json = "{}"
# create an instance of WarmPoolSpecTemplateSpecVolumesInnerQuobyte from a JSON string
warm_pool_spec_template_spec_volumes_inner_quobyte_instance = WarmPoolSpecTemplateSpecVolumesInnerQuobyte.from_json(json)
# print the JSON string representation of the object
print(WarmPoolSpecTemplateSpecVolumesInnerQuobyte.to_json())

# convert the object into a dict
warm_pool_spec_template_spec_volumes_inner_quobyte_dict = warm_pool_spec_template_spec_volumes_inner_quobyte_instance.to_dict()
# create an instance of WarmPoolSpecTemplateSpecVolumesInnerQuobyte from a dict
warm_pool_spec_template_spec_volumes_inner_quobyte_from_dict = WarmPoolSpecTemplateSpecVolumesInnerQuobyte.from_dict(warm_pool_spec_template_spec_volumes_inner_quobyte_dict)
```
[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


