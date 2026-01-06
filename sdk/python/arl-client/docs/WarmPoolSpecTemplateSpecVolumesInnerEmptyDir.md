# WarmPoolSpecTemplateSpecVolumesInnerEmptyDir


## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**medium** | **str** |  | [optional] 
**size_limit** | [**SandboxSpecResourcesLimitsValue**](SandboxSpecResourcesLimitsValue.md) |  | [optional] 

## Example

```python
from arl_client.models.warm_pool_spec_template_spec_volumes_inner_empty_dir import WarmPoolSpecTemplateSpecVolumesInnerEmptyDir

# TODO update the JSON string below
json = "{}"
# create an instance of WarmPoolSpecTemplateSpecVolumesInnerEmptyDir from a JSON string
warm_pool_spec_template_spec_volumes_inner_empty_dir_instance = WarmPoolSpecTemplateSpecVolumesInnerEmptyDir.from_json(json)
# print the JSON string representation of the object
print(WarmPoolSpecTemplateSpecVolumesInnerEmptyDir.to_json())

# convert the object into a dict
warm_pool_spec_template_spec_volumes_inner_empty_dir_dict = warm_pool_spec_template_spec_volumes_inner_empty_dir_instance.to_dict()
# create an instance of WarmPoolSpecTemplateSpecVolumesInnerEmptyDir from a dict
warm_pool_spec_template_spec_volumes_inner_empty_dir_from_dict = WarmPoolSpecTemplateSpecVolumesInnerEmptyDir.from_dict(warm_pool_spec_template_spec_volumes_inner_empty_dir_dict)
```
[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


