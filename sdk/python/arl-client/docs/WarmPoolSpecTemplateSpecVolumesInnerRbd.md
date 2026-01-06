# WarmPoolSpecTemplateSpecVolumesInnerRbd


## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**fs_type** | **str** |  | [optional] 
**image** | **str** |  | 
**keyring** | **str** |  | [optional] [default to '/etc/ceph/keyring']
**monitors** | **List[str]** |  | 
**pool** | **str** |  | [optional] [default to 'rbd']
**read_only** | **bool** |  | [optional] 
**secret_ref** | [**WarmPoolSpecTemplateSpecImagePullSecretsInner**](WarmPoolSpecTemplateSpecImagePullSecretsInner.md) |  | [optional] 
**user** | **str** |  | [optional] [default to 'admin']

## Example

```python
from arl_client.models.warm_pool_spec_template_spec_volumes_inner_rbd import WarmPoolSpecTemplateSpecVolumesInnerRbd

# TODO update the JSON string below
json = "{}"
# create an instance of WarmPoolSpecTemplateSpecVolumesInnerRbd from a JSON string
warm_pool_spec_template_spec_volumes_inner_rbd_instance = WarmPoolSpecTemplateSpecVolumesInnerRbd.from_json(json)
# print the JSON string representation of the object
print(WarmPoolSpecTemplateSpecVolumesInnerRbd.to_json())

# convert the object into a dict
warm_pool_spec_template_spec_volumes_inner_rbd_dict = warm_pool_spec_template_spec_volumes_inner_rbd_instance.to_dict()
# create an instance of WarmPoolSpecTemplateSpecVolumesInnerRbd from a dict
warm_pool_spec_template_spec_volumes_inner_rbd_from_dict = WarmPoolSpecTemplateSpecVolumesInnerRbd.from_dict(warm_pool_spec_template_spec_volumes_inner_rbd_dict)
```
[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


