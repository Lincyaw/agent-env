# WarmPoolSpecTemplateSpecVolumesInnerCephfs


## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**monitors** | **List[str]** |  | 
**path** | **str** |  | [optional] 
**read_only** | **bool** |  | [optional] 
**secret_file** | **str** |  | [optional] 
**secret_ref** | [**WarmPoolSpecTemplateSpecImagePullSecretsInner**](WarmPoolSpecTemplateSpecImagePullSecretsInner.md) |  | [optional] 
**user** | **str** |  | [optional] 

## Example

```python
from arl_client.models.warm_pool_spec_template_spec_volumes_inner_cephfs import WarmPoolSpecTemplateSpecVolumesInnerCephfs

# TODO update the JSON string below
json = "{}"
# create an instance of WarmPoolSpecTemplateSpecVolumesInnerCephfs from a JSON string
warm_pool_spec_template_spec_volumes_inner_cephfs_instance = WarmPoolSpecTemplateSpecVolumesInnerCephfs.from_json(json)
# print the JSON string representation of the object
print(WarmPoolSpecTemplateSpecVolumesInnerCephfs.to_json())

# convert the object into a dict
warm_pool_spec_template_spec_volumes_inner_cephfs_dict = warm_pool_spec_template_spec_volumes_inner_cephfs_instance.to_dict()
# create an instance of WarmPoolSpecTemplateSpecVolumesInnerCephfs from a dict
warm_pool_spec_template_spec_volumes_inner_cephfs_from_dict = WarmPoolSpecTemplateSpecVolumesInnerCephfs.from_dict(warm_pool_spec_template_spec_volumes_inner_cephfs_dict)
```
[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


