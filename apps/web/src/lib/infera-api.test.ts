import { describe, expect, it } from 'vitest'
import * as api from './infera-api'

describe('infera-api 编排端点表面（INFERA-181：全局默认编排已删除）', () => {
  it('不再导出全局默认编排 API（getPipeline/putPipeline 随 /api/pipeline 一并移除）', () => {
    expect('getPipeline' in api).toBe(false)
    expect('putPipeline' in api).toBe(false)
  })

  it('项目级编排 API 保留（getProjectPipeline/putProjectPipeline/listAgents）', () => {
    expect(typeof api.getProjectPipeline).toBe('function')
    expect(typeof api.putProjectPipeline).toBe('function')
    expect(typeof api.listAgents).toBe('function')
  })
})

describe('infera-api 死端点清理（INFERA-213：前端无调用方的 client 函数移除）', () => {
  it('不再导出 createDelivery（POST /api/projects/{id}/deliveries 无调用方）', () => {
    expect('createDelivery' in api).toBe(false)
  })

  it('不再导出 listProjectDeliveries（GET /api/projects/{id}/deliveries 生产代码已不调用）', () => {
    expect('listProjectDeliveries' in api).toBe(false)
  })
})
