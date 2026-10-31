import {newIdempotencyKey} from "@/api/idempotency";
import {ApiProblem} from "@/api/problem";
// Unknown outcomes retain the key and exact payload. Confirmed client errors have
// no successful response to reconcile, so a corrected form can use a fresh key.
export function createRetryingMutation(){
 const pending=new Map<string,{signature:string;key:string}>();
 const call=async<T>(operation:string,body:unknown,call:(key:string)=>Promise<T>):Promise<T>=>{
  const signature=JSON.stringify(body);
  let attempt=pending.get(operation);
  if(attempt && attempt.signature!==signature)throw new Error("Thao tác trước chưa được đối chiếu. Gửi lại nội dung cũ để xác nhận kết quả.");
  if(!attempt){attempt={signature,key:newIdempotencyKey()};pending.set(operation,attempt);}
  const key=attempt.key;
  try {const value=await call(key);if(pending.get(operation)?.key===key)pending.delete(operation);return value;}
  catch(error){if(error instanceof ApiProblem && error.problem.status>=400 && error.problem.status<500 && pending.get(operation)?.key===key)pending.delete(operation);throw error;}
 };
 return Object.assign(call,{resolved:(operation:string)=>pending.delete(operation)});
}
