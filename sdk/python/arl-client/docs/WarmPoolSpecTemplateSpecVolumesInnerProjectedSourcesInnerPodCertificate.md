# WarmPoolSpecTemplateSpecVolumesInnerProjectedSourcesInnerPodCertificate


## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**certificate_chain_path** | **str** |  | [optional] 
**credential_bundle_path** | **str** |  | [optional] 
**key_path** | **str** |  | [optional] 
**key_type** | **str** |  | 
**max_expiration_seconds** | **int** |  | [optional] 
**signer_name** | **str** |  | 
**user_annotations** | **Dict[str, str]** |  | [optional] 

## Example

```python
from arl_client.models.warm_pool_spec_template_spec_volumes_inner_projected_sources_inner_pod_certificate import WarmPoolSpecTemplateSpecVolumesInnerProjectedSourcesInnerPodCertificate

# TODO update the JSON string below
json = "{}"
# create an instance of WarmPoolSpecTemplateSpecVolumesInnerProjectedSourcesInnerPodCertificate from a JSON string
warm_pool_spec_template_spec_volumes_inner_projected_sources_inner_pod_certificate_instance = WarmPoolSpecTemplateSpecVolumesInnerProjectedSourcesInnerPodCertificate.from_json(json)
# print the JSON string representation of the object
print(WarmPoolSpecTemplateSpecVolumesInnerProjectedSourcesInnerPodCertificate.to_json())

# convert the object into a dict
warm_pool_spec_template_spec_volumes_inner_projected_sources_inner_pod_certificate_dict = warm_pool_spec_template_spec_volumes_inner_projected_sources_inner_pod_certificate_instance.to_dict()
# create an instance of WarmPoolSpecTemplateSpecVolumesInnerProjectedSourcesInnerPodCertificate from a dict
warm_pool_spec_template_spec_volumes_inner_projected_sources_inner_pod_certificate_from_dict = WarmPoolSpecTemplateSpecVolumesInnerProjectedSourcesInnerPodCertificate.from_dict(warm_pool_spec_template_spec_volumes_inner_projected_sources_inner_pod_certificate_dict)
```
[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


