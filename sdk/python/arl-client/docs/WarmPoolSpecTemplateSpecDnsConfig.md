# WarmPoolSpecTemplateSpecDnsConfig


## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**nameservers** | **List[str]** |  | [optional] 
**options** | [**List[WarmPoolSpecTemplateSpecDnsConfigOptionsInner]**](WarmPoolSpecTemplateSpecDnsConfigOptionsInner.md) |  | [optional] 
**searches** | **List[str]** |  | [optional] 

## Example

```python
from arl_client.models.warm_pool_spec_template_spec_dns_config import WarmPoolSpecTemplateSpecDnsConfig

# TODO update the JSON string below
json = "{}"
# create an instance of WarmPoolSpecTemplateSpecDnsConfig from a JSON string
warm_pool_spec_template_spec_dns_config_instance = WarmPoolSpecTemplateSpecDnsConfig.from_json(json)
# print the JSON string representation of the object
print(WarmPoolSpecTemplateSpecDnsConfig.to_json())

# convert the object into a dict
warm_pool_spec_template_spec_dns_config_dict = warm_pool_spec_template_spec_dns_config_instance.to_dict()
# create an instance of WarmPoolSpecTemplateSpecDnsConfig from a dict
warm_pool_spec_template_spec_dns_config_from_dict = WarmPoolSpecTemplateSpecDnsConfig.from_dict(warm_pool_spec_template_spec_dns_config_dict)
```
[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)


