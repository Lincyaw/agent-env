# WarmPoolSpecTemplateSpecTopologySpreadConstraintsInner


## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**label_selector** | [**WarmPoolSpecTemplateSpecAffinityPodAffinityPreferredDuringSchedulingIgnoredDuringExecutionInnerPodAffinityTermLabelSelector**](WarmPoolSpecTemplateSpecAffinityPodAffinityPreferredDuringSchedulingIgnoredDuringExecutionInnerPodAffinityTermLabelSelector.md) |  | [optional] 
**match_label_keys** | **List[str]** |  | [optional] 
**max_skew** | **int** |  | 
**min_domains** | **int** |  | [optional] 
**node_affinity_policy** | **str** |  | [optional] 
**node_taints_policy** | **str** |  | [optional] 
**topology_key** | **str** |  | 
**when_unsatisfiable** | **str** |  | 

## Example

```python
from arl_client.models.warm_pool_spec_template_spec_topology_spread_constraints_inner import WarmPoolSpecTemplateSpecTopologySpreadConstraintsInner

# TODO update the JSON string below
json = "{}"
# create an instance of WarmPoolSpecTemplateSpecTopologySpreadConstraintsInner from a JSON string
warm_pool_spec_template_spec_topology_spread_constraints_inner_instance = WarmPoolSpecTemplateSpecTopologySpreadConstraintsInner.from_json(json)
# print the JSON string representation of the object
print(WarmPoolSpecTemplateSpecTopologySpreadConstraintsInner.to_json())

# convert the object into a dict
warm_pool_spec_template_spec_topology_spread_constraints_inner_dict = warm_pool_spec_template_spec_topology_spread_constraints_inner_instance.to_dict()
# create an instance of WarmPoolSpecTemplateSpecTopologySpreadConstraintsInner from a dict
warm_pool_spec_template_spec_topology_spread_constraints_inner_from_dict = WarmPoolSpecTemplateSpecTopologySpreadConstraintsInner.from_dict(warm_pool_spec_template_spec_topology_spread_constraints_inner_dict)
```
[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


