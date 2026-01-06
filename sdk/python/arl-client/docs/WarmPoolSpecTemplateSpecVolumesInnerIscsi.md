# WarmPoolSpecTemplateSpecVolumesInnerIscsi


## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**chap_auth_discovery** | **bool** |  | [optional] 
**chap_auth_session** | **bool** |  | [optional] 
**fs_type** | **str** |  | [optional] 
**initiator_name** | **str** |  | [optional] 
**iqn** | **str** |  | 
**iscsi_interface** | **str** |  | [optional] [default to 'default']
**lun** | **int** |  | 
**portals** | **List[str]** |  | [optional] 
**read_only** | **bool** |  | [optional] 
**secret_ref** | [**WarmPoolSpecTemplateSpecImagePullSecretsInner**](WarmPoolSpecTemplateSpecImagePullSecretsInner.md) |  | [optional] 
**target_portal** | **str** |  | 

## Example

```python
from arl_client.models.warm_pool_spec_template_spec_volumes_inner_iscsi import WarmPoolSpecTemplateSpecVolumesInnerIscsi

# TODO update the JSON string below
json = "{}"
# create an instance of WarmPoolSpecTemplateSpecVolumesInnerIscsi from a JSON string
warm_pool_spec_template_spec_volumes_inner_iscsi_instance = WarmPoolSpecTemplateSpecVolumesInnerIscsi.from_json(json)
# print the JSON string representation of the object
print(WarmPoolSpecTemplateSpecVolumesInnerIscsi.to_json())

# convert the object into a dict
warm_pool_spec_template_spec_volumes_inner_iscsi_dict = warm_pool_spec_template_spec_volumes_inner_iscsi_instance.to_dict()
# create an instance of WarmPoolSpecTemplateSpecVolumesInnerIscsi from a dict
warm_pool_spec_template_spec_volumes_inner_iscsi_from_dict = WarmPoolSpecTemplateSpecVolumesInnerIscsi.from_dict(warm_pool_spec_template_spec_volumes_inner_iscsi_dict)
```
[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


