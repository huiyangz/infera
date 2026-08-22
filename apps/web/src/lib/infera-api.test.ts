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
