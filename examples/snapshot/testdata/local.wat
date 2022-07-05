(module
  (func (export "entry") (param $x i32) (param $y i32) (result i32)
    (local $ret i32) 
    
    nop

    i32.const 666
    local.set $ret
  
    nop
  
    local.get $ret
  )
)
