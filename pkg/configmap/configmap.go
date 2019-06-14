/**
 * Copyright 2018 Curtis Mattoon
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *  http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */
 package configmap

 import (
	 "errors"
	 "fmt"
	 "strings"

	 log "github.com/sirupsen/logrus"

	 anno "github.com/cmattoon/aws-ssm/pkg/annotations"
	 "github.com/cmattoon/aws-ssm/pkg/provider"
	 v1 "k8s.io/api/core/v1"
	 "k8s.io/client-go/kubernetes"
 )

 type ConfigMap struct {
	 ConfigMap v1.ConfigMap
	 // Kubernetes ConfigMap Name
	 Name string
	 // Kubernetes Namespace
	 Namespace string
	 // AWS Param Name
	 ParamName string
	 // AWS Param Type
	 ParamType string
	 // AWS Param Key (Default: "alias/aws/ssm")
	 ParamKey string
	 // AWS Param Value
	 ParamValue string
	 // The data to add to Kubernetes ConfigMap Data
	 Data map[string]string
 }

 func NewConfigMap(sec v1.ConfigMap, p provider.Provider, configmap_name string, configmap_namespace string, param_name string, param_type string, param_key string) (*ConfigMap, error) {

	 s := &ConfigMap{
		 ConfigMap:     sec,
		 Name:       configmap_name,
		 Namespace:  configmap_namespace,
		 ParamName:  param_name,
		 ParamType:  param_type,
		 ParamKey:   param_key,
		 ParamValue: "",
		 Data:       map[string]string{},
	 }

	 log.Debugf("Getting value for '%s/%s'", s.Namespace, s.Name)

	 decrypt := false
	 if s.ParamKey != "" {
		 decrypt = true
	 }

	 if s.ParamType == "String" || s.ParamType == "SecureString" {
		 value, err := p.GetParameterValue(s.ParamName, decrypt)
		 if err != nil {
			 return nil, err
		 }
		 s.ParamValue = value
	 } else if s.ParamType == "StringList" {
		 value, err := p.GetParameterValue(s.ParamName, decrypt)
		 if err != nil {
			 return nil, err
		 }
		 s.ParamValue = value
		 // StringList: Also set each key
		 values := s.ParseStringList()
		 for k, v := range values {
			 s.Set(k, v)
		 }
	 } else if s.ParamType == "Directory" {
		 // Directory: Set each sub-key
		 all_params, err := p.GetParameterDataByPath(s.ParamName, decrypt)
		 if err != nil {
			 return nil, err
		 }

		 for k, v := range all_params {
			 s.Set(safeKeyName(k), v)
		 }
		 s.ParamValue = "true" // Reads "Directory": "true"
		 return s, nil
	 }

	 // Always set the "$ParamType" key:
	 //   String: Value
	 //   SecureString: Value
	 //   StringList: Value
	 //   Directory: <ssm-path>
	 s.Set(s.ParamType, s.ParamValue)

	 return s, nil
 }

 // FromKubernetesConfigMap returns an internal ConfigMap struct, if the v1.ConfigMap is properly annotated.
 func FromKubernetesConfigMap(p provider.Provider, configmap v1.ConfigMap) (*ConfigMap, error) {
	 param_name := ""
	 param_type := ""
	 param_key := ""

	 for k, v := range configmap.ObjectMeta.Annotations {
		 switch k {
		 case anno.AWSParamName, anno.V1ParamName:
			 param_name = v
		 case anno.AWSParamType, anno.V1ParamType:
			 param_type = v
		 case anno.AWSParamKey, anno.V1ParamKey:
			 param_key = v
		 }
	 }

	 if param_name == "" || param_type == "" {
		 return nil, errors.New("Irrelevant ConfigMap")
	 }

	 if param_name != "" && param_type != "" {
		 if param_type == "SecureString" && param_key == "" {
			 log.Info("No KMS key defined. Using default key 'alias/aws/ssm'")
			 param_key = "alias/aws/ssm"
		 }
	 }

	 s, err := NewConfigMap(
		 configmap,
		 p,
		 configmap.ObjectMeta.Name,
		 configmap.ObjectMeta.Namespace,
		 param_name,
		 param_type,
		 param_key)

	 if err != nil {
		 return nil, err
	 }
	 return s, nil
 }

 func (s *ConfigMap) ParseStringList() (values map[string]string) {
	 values = make(map[string]string)

	 for _, pair := range strings.Split(strings.TrimSpace(s.ParamValue), ",") {
		 pair = strings.TrimSpace(pair)
		 key := pair
		 val := ""

		 if strings.Contains(pair, "=") {
			 kv := strings.SplitN(pair, "=", 2)
			 if len(kv) == 2 {
				 if kv[0] != "" {
					 key = kv[0]
					 val = kv[1]
				 }
			 }
		 }
		 if key != "" {
			 values[key] = val
		 }
	 }

	 return
 }

 func (s *ConfigMap) Set(key string, val string) (err error) {
	 log.Debugf("Setting key=%s", key)
	 if s.ConfigMap.Data == nil {
		 s.ConfigMap.Data = make(map[string]string)
	 }
	 // Data isn't populated initially, so check s.Data
	 if _, ok := s.Data[key]; ok {
		 // Refuse to overwite existing keys
		 return errors.New(fmt.Sprintf("Key '%s' already exists for ConfigMap %s/%s", key, s.Namespace, s.Name))
	 }
	 s.ConfigMap.Data[key] = val
	 return
 }

 func (s *ConfigMap) UpdateObject(cli kubernetes.Interface) (result *v1.ConfigMap, err error) {
	 log.Info("Updating Kubernetes ConfigMap...")
	 return cli.CoreV1().ConfigMaps(s.Namespace).Update(&s.ConfigMap)
 }

 func safeKeyName(key string) string {
	 key = strings.TrimRight(key, "/")
	 if strings.HasPrefix(key, "/") {
		 key = strings.Replace(key, "/", "", 1)
	 }
	 return strings.Replace(key, "/", "_", -1)
 }
