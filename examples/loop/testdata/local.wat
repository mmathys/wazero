(module
  (func (export "add3") (param $x i32) (result i32)
    (local $ret i32) 
    
    nop

    i32.const 666
    local.set $ret
    
    nop 

    local.get $ret
  )
)
