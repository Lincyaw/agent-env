# WarmPoolSpecTemplateSpecVolumesInnerAzureFile


## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**read_only** | **bool** |  | [optional] 
**secret_name** | **str** |  | 
**share_name** | **str** |  | 

## Example

```python
from arl_client.models.warm_pool_spec_template_spec_volumes_inner_azure_file import WarmPoolSpecTemplateSpecVolumesInnerAzureFile

# TODO update the JSON string below
json = "{}"
# create an instance of WarmPoolSpecTemplateSpecVolumesInnerAzureFile from a JSON string
warm_pool_spec_template_spec_volumes_inner_azure_file_instance = WarmPoolSpecTemplateSpecVolumesInnerAzureFile.from_json(json)
# print the JSON string representation of the object
print(WarmPoolSpecTemplateSpecVolumesInnerAzureFile.to_json())

# convert the object into a dict
warm_pool_spec_template_spec_volumes_inner_azure_file_dict = warm_pool_spec_template_spec_volumes_inner_azure_file_instance.to_dict()
# create an instance of WarmPoolSpecTemplateSpecVolumesInnerAzureFile from a dict
warm_pool_spec_template_spec_volumes_inner_azure_file_from_dict = WarmPoolSpecTemplateSpecVolumesInnerAzureFile.from_dict(warm_pool_spec_template_spec_volumes_inner_azure_file_dict)
```
[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


