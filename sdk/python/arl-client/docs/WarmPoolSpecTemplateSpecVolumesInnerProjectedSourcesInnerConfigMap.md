# WarmPoolSpecTemplateSpecVolumesInnerProjectedSourcesInnerConfigMap


## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**items** | [**List[WarmPoolSpecTemplateSpecVolumesInnerConfigMapItemsInner]**](WarmPoolSpecTemplateSpecVolumesInnerConfigMapItemsInner.md) |  | [optional] 
**name** | **str** |  | [optional] [default to '']
**optional** | **bool** |  | [optional] 

## Example

```python
from arl_client.models.warm_pool_spec_template_spec_volumes_inner_projected_sources_inner_config_map import WarmPoolSpecTemplateSpecVolumesInnerProjectedSourcesInnerConfigMap

# TODO update the JSON string below
json = "{}"
# create an instance of WarmPoolSpecTemplateSpecVolumesInnerProjectedSourcesInnerConfigMap from a JSON string
warm_pool_spec_template_spec_volumes_inner_projected_sources_inner_config_map_instance = WarmPoolSpecTemplateSpecVolumesInnerProjectedSourcesInnerConfigMap.from_json(json)
# print the JSON string representation of the object
print(WarmPoolSpecTemplateSpecVolumesInnerProjectedSourcesInnerConfigMap.to_json())

# convert the object into a dict
warm_pool_spec_template_spec_volumes_inner_projected_sources_inner_config_map_dict = warm_pool_spec_template_spec_volumes_inner_projected_sources_inner_config_map_instance.to_dict()
# create an instance of WarmPoolSpecTemplateSpecVolumesInnerProjectedSourcesInnerConfigMap from a dict
warm_pool_spec_template_spec_volumes_inner_projected_sources_inner_config_map_from_dict = WarmPoolSpecTemplateSpecVolumesInnerProjectedSourcesInnerConfigMap.from_dict(warm_pool_spec_template_spec_volumes_inner_projected_sources_inner_config_map_dict)
```
[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


