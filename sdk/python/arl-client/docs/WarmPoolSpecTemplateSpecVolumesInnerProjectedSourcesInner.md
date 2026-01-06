# WarmPoolSpecTemplateSpecVolumesInnerProjectedSourcesInner


## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**cluster_trust_bundle** | [**WarmPoolSpecTemplateSpecVolumesInnerProjectedSourcesInnerClusterTrustBundle**](WarmPoolSpecTemplateSpecVolumesInnerProjectedSourcesInnerClusterTrustBundle.md) |  | [optional] 
**config_map** | [**WarmPoolSpecTemplateSpecVolumesInnerProjectedSourcesInnerConfigMap**](WarmPoolSpecTemplateSpecVolumesInnerProjectedSourcesInnerConfigMap.md) |  | [optional] 
**downward_api** | [**WarmPoolSpecTemplateSpecVolumesInnerProjectedSourcesInnerDownwardAPI**](WarmPoolSpecTemplateSpecVolumesInnerProjectedSourcesInnerDownwardAPI.md) |  | [optional] 
**pod_certificate** | [**WarmPoolSpecTemplateSpecVolumesInnerProjectedSourcesInnerPodCertificate**](WarmPoolSpecTemplateSpecVolumesInnerProjectedSourcesInnerPodCertificate.md) |  | [optional] 
**secret** | [**WarmPoolSpecTemplateSpecVolumesInnerProjectedSourcesInnerConfigMap**](WarmPoolSpecTemplateSpecVolumesInnerProjectedSourcesInnerConfigMap.md) |  | [optional] 
**service_account_token** | [**WarmPoolSpecTemplateSpecVolumesInnerProjectedSourcesInnerServiceAccountToken**](WarmPoolSpecTemplateSpecVolumesInnerProjectedSourcesInnerServiceAccountToken.md) |  | [optional] 

## Example

```python
from arl_client.models.warm_pool_spec_template_spec_volumes_inner_projected_sources_inner import WarmPoolSpecTemplateSpecVolumesInnerProjectedSourcesInner

# TODO update the JSON string below
json = "{}"
# create an instance of WarmPoolSpecTemplateSpecVolumesInnerProjectedSourcesInner from a JSON string
warm_pool_spec_template_spec_volumes_inner_projected_sources_inner_instance = WarmPoolSpecTemplateSpecVolumesInnerProjectedSourcesInner.from_json(json)
# print the JSON string representation of the object
print(WarmPoolSpecTemplateSpecVolumesInnerProjectedSourcesInner.to_json())

# convert the object into a dict
warm_pool_spec_template_spec_volumes_inner_projected_sources_inner_dict = warm_pool_spec_template_spec_volumes_inner_projected_sources_inner_instance.to_dict()
# create an instance of WarmPoolSpecTemplateSpecVolumesInnerProjectedSourcesInner from a dict
warm_pool_spec_template_spec_volumes_inner_projected_sources_inner_from_dict = WarmPoolSpecTemplateSpecVolumesInnerProjectedSourcesInner.from_dict(warm_pool_spec_template_spec_volumes_inner_projected_sources_inner_dict)
```
[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


