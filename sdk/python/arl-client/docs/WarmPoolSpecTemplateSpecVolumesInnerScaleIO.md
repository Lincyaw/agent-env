# WarmPoolSpecTemplateSpecVolumesInnerScaleIO


## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**fs_type** | **str** |  | [optional] [default to 'xfs']
**gateway** | **str** |  | 
**protection_domain** | **str** |  | [optional] 
**read_only** | **bool** |  | [optional] 
**secret_ref** | [**WarmPoolSpecTemplateSpecImagePullSecretsInner**](WarmPoolSpecTemplateSpecImagePullSecretsInner.md) |  | 
**ssl_enabled** | **bool** |  | [optional] 
**storage_mode** | **str** |  | [optional] [default to 'ThinProvisioned']
**storage_pool** | **str** |  | [optional] 
**system** | **str** |  | 
**volume_name** | **str** |  | [optional] 

## Example

```python
from arl_client.models.warm_pool_spec_template_spec_volumes_inner_scale_io import WarmPoolSpecTemplateSpecVolumesInnerScaleIO

# TODO update the JSON string below
json = "{}"
# create an instance of WarmPoolSpecTemplateSpecVolumesInnerScaleIO from a JSON string
warm_pool_spec_template_spec_volumes_inner_scale_io_instance = WarmPoolSpecTemplateSpecVolumesInnerScaleIO.from_json(json)
# print the JSON string representation of the object
print(WarmPoolSpecTemplateSpecVolumesInnerScaleIO.to_json())

# convert the object into a dict
warm_pool_spec_template_spec_volumes_inner_scale_io_dict = warm_pool_spec_template_spec_volumes_inner_scale_io_instance.to_dict()
# create an instance of WarmPoolSpecTemplateSpecVolumesInnerScaleIO from a dict
warm_pool_spec_template_spec_volumes_inner_scale_io_from_dict = WarmPoolSpecTemplateSpecVolumesInnerScaleIO.from_dict(warm_pool_spec_template_spec_volumes_inner_scale_io_dict)
```
[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


