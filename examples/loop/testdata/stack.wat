(module
  (func (export "add3") (param $x i32) (result i32)
    local.get $x
    
    i32.const 1
    i32.add

    i32.const 1
    i32.add

    i32.const 1
    i32.add
  )
)
