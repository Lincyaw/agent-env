# WarmPoolSpecTemplateSpecVolumesInnerEphemeralVolumeClaimTemplateSpec


## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**access_modes** | **List[str]** |  | [optional] 
**data_source** | [**WarmPoolSpecTemplateSpecVolumesInnerEphemeralVolumeClaimTemplateSpecDataSource**](WarmPoolSpecTemplateSpecVolumesInnerEphemeralVolumeClaimTemplateSpecDataSource.md) |  | [optional] 
**data_source_ref** | [**WarmPoolSpecTemplateSpecVolumesInnerEphemeralVolumeClaimTemplateSpecDataSourceRef**](WarmPoolSpecTemplateSpecVolumesInnerEphemeralVolumeClaimTemplateSpecDataSourceRef.md) |  | [optional] 
**resources** | [**WarmPoolSpecTemplateSpecVolumesInnerEphemeralVolumeClaimTemplateSpecResources**](WarmPoolSpecTemplateSpecVolumesInnerEphemeralVolumeClaimTemplateSpecResources.md) |  | [optional] 
**selector** | [**WarmPoolSpecTemplateSpecAffinityPodAffinityPreferredDuringSchedulingIgnoredDuringExecutionInnerPodAffinityTermLabelSelector**](WarmPoolSpecTemplateSpecAffinityPodAffinityPreferredDuringSchedulingIgnoredDuringExecutionInnerPodAffinityTermLabelSelector.md) |  | [optional] 
**storage_class_name** | **str** |  | [optional] 
**volume_attributes_class_name** | **str** |  | [optional] 
**volume_mode** | **str** |  | [optional] 
**volume_name** | **str** |  | [optional] 

## Example

```python
from arl_client.models.warm_pool_spec_template_spec_volumes_inner_ephemeral_volume_claim_template_spec import WarmPoolSpecTemplateSpecVolumesInnerEphemeralVolumeClaimTemplateSpec

# TODO update the JSON string below
json = "{}"
# create an instance of WarmPoolSpecTemplateSpecVolumesInnerEphemeralVolumeClaimTemplateSpec from a JSON string
warm_pool_spec_template_spec_volumes_inner_ephemeral_volume_claim_template_spec_instance = WarmPoolSpecTemplateSpecVolumesInnerEphemeralVolumeClaimTemplateSpec.from_json(json)
# print the JSON string representation of the object
print(WarmPoolSpecTemplateSpecVolumesInnerEphemeralVolumeClaimTemplateSpec.to_json())

# convert the object into a dict
warm_pool_spec_template_spec_volumes_inner_ephemeral_volume_claim_template_spec_dict = warm_pool_spec_template_spec_volumes_inner_ephemeral_volume_claim_template_spec_instance.to_dict()
# create an instance of WarmPoolSpecTemplateSpecVolumesInnerEphemeralVolumeClaimTemplateSpec from a dict
warm_pool_spec_template_spec_volumes_inner_ephemeral_volume_claim_template_spec_from_dict = WarmPoolSpecTemplateSpecVolumesInnerEphemeralVolumeClaimTemplateSpec.from_dict(warm_pool_spec_template_spec_volumes_inner_ephemeral_volume_claim_template_spec_dict)
```
[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


