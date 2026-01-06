# WarmPoolSpecTemplateSpecVolumesInnerFlexVolume


## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**driver** | **str** |  | 
**fs_type** | **str** |  | [optional] 
**options** | **Dict[str, str]** |  | [optional] 
**read_only** | **bool** |  | [optional] 
**secret_ref** | [**WarmPoolSpecTemplateSpecImagePullSecretsInner**](WarmPoolSpecTemplateSpecImagePullSecretsInner.md) |  | [optional] 

## Example

```python
from arl_client.models.warm_pool_spec_template_spec_volumes_inner_flex_volume import WarmPoolSpecTemplateSpecVolumesInnerFlexVolume

# TODO update the JSON string below
json = "{}"
# create an instance of WarmPoolSpecTemplateSpecVolumesInnerFlexVolume from a JSON string
warm_pool_spec_template_spec_volumes_inner_flex_volume_instance = WarmPoolSpecTemplateSpecVolumesInnerFlexVolume.from_json(json)
# print the JSON string representation of the object
print(WarmPoolSpecTemplateSpecVolumesInnerFlexVolume.to_json())

# convert the object into a dict
warm_pool_spec_template_spec_volumes_inner_flex_volume_dict = warm_pool_spec_template_spec_volumes_inner_flex_volume_instance.to_dict()
# create an instance of WarmPoolSpecTemplateSpecVolumesInnerFlexVolume from a dict
warm_pool_spec_template_spec_volumes_inner_flex_volume_from_dict = WarmPoolSpecTemplateSpecVolumesInnerFlexVolume.from_dict(warm_pool_spec_template_spec_volumes_inner_flex_volume_dict)
```
[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


