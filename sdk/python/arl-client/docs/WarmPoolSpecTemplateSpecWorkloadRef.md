# WarmPoolSpecTemplateSpecWorkloadRef


## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**name** | **str** |  | 
**pod_group** | **str** |  | 
**pod_group_replica_key** | **str** |  | [optional] 

## Example

```python
from arl_client.models.warm_pool_spec_template_spec_workload_ref import WarmPoolSpecTemplateSpecWorkloadRef

# TODO update the JSON string below
json = "{}"
# create an instance of WarmPoolSpecTemplateSpecWorkloadRef from a JSON string
warm_pool_spec_template_spec_workload_ref_instance = WarmPoolSpecTemplateSpecWorkloadRef.from_json(json)
# print the JSON string representation of the object
print(WarmPoolSpecTemplateSpecWorkloadRef.to_json())

# convert the object into a dict
warm_pool_spec_template_spec_workload_ref_dict = warm_pool_spec_template_spec_workload_ref_instance.to_dict()
# create an instance of WarmPoolSpecTemplateSpecWorkloadRef from a dict
warm_pool_spec_template_spec_workload_ref_from_dict = WarmPoolSpecTemplateSpecWorkloadRef.from_dict(warm_pool_spec_template_spec_workload_ref_dict)
```
[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


