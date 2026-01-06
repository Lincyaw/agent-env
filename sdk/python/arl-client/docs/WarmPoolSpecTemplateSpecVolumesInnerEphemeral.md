# WarmPoolSpecTemplateSpecVolumesInnerEphemeral


## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**volume_claim_template** | [**WarmPoolSpecTemplateSpecVolumesInnerEphemeralVolumeClaimTemplate**](WarmPoolSpecTemplateSpecVolumesInnerEphemeralVolumeClaimTemplate.md) |  | [optional] 

## Example

```python
from arl_client.models.warm_pool_spec_template_spec_volumes_inner_ephemeral import WarmPoolSpecTemplateSpecVolumesInnerEphemeral

# TODO update the JSON string below
json = "{}"
# create an instance of WarmPoolSpecTemplateSpecVolumesInnerEphemeral from a JSON string
warm_pool_spec_template_spec_volumes_inner_ephemeral_instance = WarmPoolSpecTemplateSpecVolumesInnerEphemeral.from_json(json)
# print the JSON string representation of the object
print(WarmPoolSpecTemplateSpecVolumesInnerEphemeral.to_json())

# convert the object into a dict
warm_pool_spec_template_spec_volumes_inner_ephemeral_dict = warm_pool_spec_template_spec_volumes_inner_ephemeral_instance.to_dict()
# create an instance of WarmPoolSpecTemplateSpecVolumesInnerEphemeral from a dict
warm_pool_spec_template_spec_volumes_inner_ephemeral_from_dict = WarmPoolSpecTemplateSpecVolumesInnerEphemeral.from_dict(warm_pool_spec_template_spec_volumes_inner_ephemeral_dict)
```
[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


