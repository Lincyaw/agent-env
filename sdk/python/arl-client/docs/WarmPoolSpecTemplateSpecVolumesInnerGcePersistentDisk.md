# WarmPoolSpecTemplateSpecVolumesInnerGcePersistentDisk


## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**fs_type** | **str** |  | [optional] 
**partition** | **int** |  | [optional] 
**pd_name** | **str** |  | 
**read_only** | **bool** |  | [optional] 

## Example

```python
from arl_client.models.warm_pool_spec_template_spec_volumes_inner_gce_persistent_disk import WarmPoolSpecTemplateSpecVolumesInnerGcePersistentDisk

# TODO update the JSON string below
json = "{}"
# create an instance of WarmPoolSpecTemplateSpecVolumesInnerGcePersistentDisk from a JSON string
warm_pool_spec_template_spec_volumes_inner_gce_persistent_disk_instance = WarmPoolSpecTemplateSpecVolumesInnerGcePersistentDisk.from_json(json)
# print the JSON string representation of the object
print(WarmPoolSpecTemplateSpecVolumesInnerGcePersistentDisk.to_json())

# convert the object into a dict
warm_pool_spec_template_spec_volumes_inner_gce_persistent_disk_dict = warm_pool_spec_template_spec_volumes_inner_gce_persistent_disk_instance.to_dict()
# create an instance of WarmPoolSpecTemplateSpecVolumesInnerGcePersistentDisk from a dict
warm_pool_spec_template_spec_volumes_inner_gce_persistent_disk_from_dict = WarmPoolSpecTemplateSpecVolumesInnerGcePersistentDisk.from_dict(warm_pool_spec_template_spec_volumes_inner_gce_persistent_disk_dict)
```
[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


