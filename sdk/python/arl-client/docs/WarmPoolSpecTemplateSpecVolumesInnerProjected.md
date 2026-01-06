# WarmPoolSpecTemplateSpecVolumesInnerProjected


## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**default_mode** | **int** |  | [optional] 
**sources** | [**List[WarmPoolSpecTemplateSpecVolumesInnerProjectedSourcesInner]**](WarmPoolSpecTemplateSpecVolumesInnerProjectedSourcesInner.md) |  | [optional] 

## Example

```python
from arl_client.models.warm_pool_spec_template_spec_volumes_inner_projected import WarmPoolSpecTemplateSpecVolumesInnerProjected

# TODO update the JSON string below
json = "{}"
# create an instance of WarmPoolSpecTemplateSpecVolumesInnerProjected from a JSON string
warm_pool_spec_template_spec_volumes_inner_projected_instance = WarmPoolSpecTemplateSpecVolumesInnerProjected.from_json(json)
# print the JSON string representation of the object
print(WarmPoolSpecTemplateSpecVolumesInnerProjected.to_json())

# convert the object into a dict
warm_pool_spec_template_spec_volumes_inner_projected_dict = warm_pool_spec_template_spec_volumes_inner_projected_instance.to_dict()
# create an instance of WarmPoolSpecTemplateSpecVolumesInnerProjected from a dict
warm_pool_spec_template_spec_volumes_inner_projected_from_dict = WarmPoolSpecTemplateSpecVolumesInnerProjected.from_dict(warm_pool_spec_template_spec_volumes_inner_projected_dict)
```
[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


