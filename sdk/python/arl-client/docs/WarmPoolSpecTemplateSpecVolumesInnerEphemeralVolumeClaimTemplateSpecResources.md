# WarmPoolSpecTemplateSpecVolumesInnerEphemeralVolumeClaimTemplateSpecResources


## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**limits** | [**Dict[str, SandboxSpecResourcesLimitsValue]**](SandboxSpecResourcesLimitsValue.md) |  | [optional] 
**requests** | [**Dict[str, SandboxSpecResourcesLimitsValue]**](SandboxSpecResourcesLimitsValue.md) |  | [optional] 

## Example

```python
from arl_client.models.warm_pool_spec_template_spec_volumes_inner_ephemeral_volume_claim_template_spec_resources import WarmPoolSpecTemplateSpecVolumesInnerEphemeralVolumeClaimTemplateSpecResources

# TODO update the JSON string below
json = "{}"
# create an instance of WarmPoolSpecTemplateSpecVolumesInnerEphemeralVolumeClaimTemplateSpecResources from a JSON string
warm_pool_spec_template_spec_volumes_inner_ephemeral_volume_claim_template_spec_resources_instance = WarmPoolSpecTemplateSpecVolumesInnerEphemeralVolumeClaimTemplateSpecResources.from_json(json)
# print the JSON string representation of the object
print(WarmPoolSpecTemplateSpecVolumesInnerEphemeralVolumeClaimTemplateSpecResources.to_json())

# convert the object into a dict
warm_pool_spec_template_spec_volumes_inner_ephemeral_volume_claim_template_spec_resources_dict = warm_pool_spec_template_spec_volumes_inner_ephemeral_volume_claim_template_spec_resources_instance.to_dict()
# create an instance of WarmPoolSpecTemplateSpecVolumesInnerEphemeralVolumeClaimTemplateSpecResources from a dict
warm_pool_spec_template_spec_volumes_inner_ephemeral_volume_claim_template_spec_resources_from_dict = WarmPoolSpecTemplateSpecVolumesInnerEphemeralVolumeClaimTemplateSpecResources.from_dict(warm_pool_spec_template_spec_volumes_inner_ephemeral_volume_claim_template_spec_resources_dict)
```
[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


