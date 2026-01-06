# WarmPoolSpecTemplateSpecContainersInnerPortsInner


## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**container_port** | **int** |  | 
**host_ip** | **str** |  | [optional] 
**host_port** | **int** |  | [optional] 
**name** | **str** |  | [optional] 
**protocol** | **str** |  | [optional] [default to 'TCP']

## Example

```python
from arl_client.models.warm_pool_spec_template_spec_containers_inner_ports_inner import WarmPoolSpecTemplateSpecContainersInnerPortsInner

# TODO update the JSON string below
json = "{}"
# create an instance of WarmPoolSpecTemplateSpecContainersInnerPortsInner from a JSON string
warm_pool_spec_template_spec_containers_inner_ports_inner_instance = WarmPoolSpecTemplateSpecContainersInnerPortsInner.from_json(json)
# print the JSON string representation of the object
print(WarmPoolSpecTemplateSpecContainersInnerPortsInner.to_json())

# convert the object into a dict
warm_pool_spec_template_spec_containers_inner_ports_inner_dict = warm_pool_spec_template_spec_containers_inner_ports_inner_instance.to_dict()
# create an instance of WarmPoolSpecTemplateSpecContainersInnerPortsInner from a dict
warm_pool_spec_template_spec_containers_inner_ports_inner_from_dict = WarmPoolSpecTemplateSpecContainersInnerPortsInner.from_dict(warm_pool_spec_template_spec_containers_inner_ports_inner_dict)
```
[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


