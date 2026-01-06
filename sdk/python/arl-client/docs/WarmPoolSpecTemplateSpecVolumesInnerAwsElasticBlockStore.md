# WarmPoolSpecTemplateSpecVolumesInnerAwsElasticBlockStore


## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**fs_type** | **str** |  | [optional] 
**partition** | **int** |  | [optional] 
**read_only** | **bool** |  | [optional] 
**volume_id** | **str** |  | 

## Example

```python
from arl_client.models.warm_pool_spec_template_spec_volumes_inner_aws_elastic_block_store import WarmPoolSpecTemplateSpecVolumesInnerAwsElasticBlockStore

# TODO update the JSON string below
json = "{}"
# create an instance of WarmPoolSpecTemplateSpecVolumesInnerAwsElasticBlockStore from a JSON string
warm_pool_spec_template_spec_volumes_inner_aws_elastic_block_store_instance = WarmPoolSpecTemplateSpecVolumesInnerAwsElasticBlockStore.from_json(json)
# print the JSON string representation of the object
print(WarmPoolSpecTemplateSpecVolumesInnerAwsElasticBlockStore.to_json())

# convert the object into a dict
warm_pool_spec_template_spec_volumes_inner_aws_elastic_block_store_dict = warm_pool_spec_template_spec_volumes_inner_aws_elastic_block_store_instance.to_dict()
# create an instance of WarmPoolSpecTemplateSpecVolumesInnerAwsElasticBlockStore from a dict
warm_pool_spec_template_spec_volumes_inner_aws_elastic_block_store_from_dict = WarmPoolSpecTemplateSpecVolumesInnerAwsElasticBlockStore.from_dict(warm_pool_spec_template_spec_volumes_inner_aws_elastic_block_store_dict)
```
[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


