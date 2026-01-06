# WarmPoolSpecTemplateSpecVolumesInnerAzureDisk


## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**caching_mode** | **str** |  | [optional] 
**disk_name** | **str** |  | 
**disk_uri** | **str** |  | 
**fs_type** | **str** |  | [optional] [default to 'ext4']
**kind** | **str** |  | [optional] 
**read_only** | **bool** |  | [optional] [default to False]

## Example

```python
from arl_client.models.warm_pool_spec_template_spec_volumes_inner_azure_disk import WarmPoolSpecTemplateSpecVolumesInnerAzureDisk

# TODO update the JSON string below
json = "{}"
# create an instance of WarmPoolSpecTemplateSpecVolumesInnerAzureDisk from a JSON string
warm_pool_spec_template_spec_volumes_inner_azure_disk_instance = WarmPoolSpecTemplateSpecVolumesInnerAzureDisk.from_json(json)
# print the JSON string representation of the object
print(WarmPoolSpecTemplateSpecVolumesInnerAzureDisk.to_json())

# convert the object into a dict
warm_pool_spec_template_spec_volumes_inner_azure_disk_dict = warm_pool_spec_template_spec_volumes_inner_azure_disk_instance.to_dict()
# create an instance of WarmPoolSpecTemplateSpecVolumesInnerAzureDisk from a dict
warm_pool_spec_template_spec_volumes_inner_azure_disk_from_dict = WarmPoolSpecTemplateSpecVolumesInnerAzureDisk.from_dict(warm_pool_spec_template_spec_volumes_inner_azure_disk_dict)
```
[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


