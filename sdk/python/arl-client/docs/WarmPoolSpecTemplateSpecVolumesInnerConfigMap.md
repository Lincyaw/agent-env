# WarmPoolSpecTemplateSpecVolumesInnerConfigMap


## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**default_mode** | **int** |  | [optional] 
**items** | [**List[WarmPoolSpecTemplateSpecVolumesInnerConfigMapItemsInner]**](WarmPoolSpecTemplateSpecVolumesInnerConfigMapItemsInner.md) |  | [optional] 
**name** | **str** |  | [optional] [default to '']
**optional** | **bool** |  | [optional] 

## Example

```python
from arl_client.models.warm_pool_spec_template_spec_volumes_inner_config_map import WarmPoolSpecTemplateSpecVolumesInnerConfigMap

# TODO update the JSON string below
json = "{}"
# create an instance of WarmPoolSpecTemplateSpecVolumesInnerConfigMap from a JSON string
warm_pool_spec_template_spec_volumes_inner_config_map_instance = WarmPoolSpecTemplateSpecVolumesInnerConfigMap.from_json(json)
# print the JSON string representation of the object
print(WarmPoolSpecTemplateSpecVolumesInnerConfigMap.to_json())

# convert the object into a dict
warm_pool_spec_template_spec_volumes_inner_config_map_dict = warm_pool_spec_template_spec_volumes_inner_config_map_instance.to_dict()
# create an instance of WarmPoolSpecTemplateSpecVolumesInnerConfigMap from a dict
warm_pool_spec_template_spec_volumes_inner_config_map_from_dict = WarmPoolSpecTemplateSpecVolumesInnerConfigMap.from_dict(warm_pool_spec_template_spec_volumes_inner_config_map_dict)
```
[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


