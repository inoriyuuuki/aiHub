# 动态提示词 Schema 规范

采用 JSON Schema 2020-12 子集 + `x-aihub-*` UI 扩展，服务端只做校验与渲染，不执行任意逻辑（无 JavaScript/公式/条件显隐）。

## 支持类型与控件
| type | x-aihub-ui | 说明 |
| --- | --- | --- |
| string | text / textarea / markdown / code | 文本类 |
| string | select / radio | 枚举选择 |
| string | model-provider / model-name | 模型建议 |
| number / integer | number | 数值（支持 minimum/maximum） |
| boolean | switch | 开关 |
| array | multi-select | 多选（enum） |
| array | repeatable-group | 可重复对象组（items 为 object） |
| object | group | 对象分组（嵌套 properties） |
| string[] | file / image / effect-file | 附件（值存 asset id） |

## 校验规则
- 顶层必须 `type: object` 且有 `properties`。
- 字段校验：必填（`required`）、默认值、枚举、长度（minLength/maxLength）、数值范围（minimum/maximum）。
- 可重复组 items 必须为 object 且含 properties；禁止嵌套无限层（本版限制为合理深度）。

## 模板变量
- 声明：schema 顶层 `x-aihub-variables: [{name, description, default}]`。
- 使用：内容字符串中 `{{variable}}`；保存时校验所有使用到的变量均已声明。
- 渲染：`POST /prompts/{id}/render` 传入 `{values}`，返回渲染后的内容；未提供的变量保持原样，不执行模型请求。

## 种子模板
初始化三个可编辑分类模板：对话提示词、生图提示词、代码提示词。
