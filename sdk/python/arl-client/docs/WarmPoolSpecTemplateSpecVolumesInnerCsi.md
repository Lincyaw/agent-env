# WarmPoolSpecTemplateSpecVolumesInnerCsi


## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**driver** | **str** |  | 
**fs_type** | **str** |  | [optional] 
**node_publish_secret_ref** | [**WarmPoolSpecTemplateSpecImagePullSecretsInner**](WarmPoolSpecTemplateSpecImagePullSecretsInner.md) |  | [optional] 
**read_only** | **bool** |  | [optional] 
**volume_attributes** | **Dict[str, str]** |  | [optional] 

## Example

```python
from arl_client.models.warm_pool_spec_template_spec_volumes_inner_csi import WarmPoolSpecTemplateSpecVolumesInnerCsi

# TODO update the JSON string below
json = "{}"
# create an instance of WarmPoolSpecTemplateSpecVolumesInnerCsi from a JSON string
warm_pool_spec_template_spec_volumes_inner_csi_instance = WarmPoolSpecTemplateSpecVolumesInnerCsi.from_json(json)
# print the JSON string representation of the object
print(WarmPoolSpecTemplateSpecVolumesInnerCsi.to_json())

# convert the object into a dict
warm_pool_spec_template_spec_volumes_inner_csi_dict = warm_pool_spec_template_spec_volumes_inner_csi_instance.to_dict()
# create an instance of WarmPoolSpecTemplateSpecVolumesInnerCsi from a dict
warm_pool_spec_template_spec_volumes_inner_csi_from_dict = WarmPoolSpecTemplateSpecVolumesInnerCsi.from_dict(warm_pool_spec_template_spec_volumes_inner_csi_dict)
```
[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


