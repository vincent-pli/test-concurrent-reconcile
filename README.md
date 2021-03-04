# test-concurrent-reconcile


In this project, I want to test two things:
- Mutiple thread reconccile
- Custom `Rate Limiter`


we try to do an abnormal thing in controller: innvoke a remote server and polling get it's state (since it's a asynchronous process).
Actually, it's not a suitable scenario for controller, like this:




The state life cycle is good, but these is one problem: the state(log) retrieval part is too heavy.
the typical process is that:

Get a CR(named Run for example) with no init state, OK, the controller will invoke remote server and write ID which is in http response to the CR and update the state of CR to `unknow`.
Since the CR changed, so k8s api-server will notify controller and a new reconcile trigered, controller see CR's state is `unknow` so it invoke the remote server try to get state and logs, but in general, the remote process is not complete, since the notification from api-server is very fast, maybe cost several millisecond. so the reconcile has to just write the log back to the CR, not change the state, but next triiger will arrive in several millisecond... the same thing happen again and again, especially the remote process is a long running one, it's wasting resource, not make sense.

A straight forward solution is adopt `Rate Limiter` in `workqueue`.
The basic idea is 
- Implement a custom `Rate Limiter`, the requeued resource will be add to the queue after 5 sec.
- If reconcile found the remote process is not complete, then it will not update anything for CR just `requeue` the CR then 5 sec later the same CR will trigger reconcile again.


The new solution will not effect k8s api-server, just requeue the `workqueue` in controller.

I think it could resolve the problem.




