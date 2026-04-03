Your challenge is to build a fees API in **encore** ([encore.dev](http://encore.dev)) that uses a **temporal** ([temporal.io](http://temporal.io)) workflow started at the beginning of a fee period, and allows for the progressive accrual of fees. At the end of the billing period, the total invoice and bill summation should be available.

**Requirements:**

1.  Able to create new bill
    
2.  Able to add line item to an existing open bill
    
3.  Able to close an active bill
    
    a. indicate total amount being charged
    
    b. indicate all line item being charged
    
4.  Reject line item addition if bill is closed (bill already charged)
    
5.  Able to query open and closed bill
    
6.  Able to handle different types of currency, for the sake of simplicity, assume GEL and USD.
    



You are free to design the RESTFul API that will be used by other services. The above requirements are not exhaustive, you’re free to add in requirements that you think are helpful in making the Fees API more flexible and complete.

  

Things to consider:

● How should money be represented?

● What are the correct semantics for the API?

● Data Modelling and Lifecycle of Entity

● What problems does Temporal solve?

  
We encourage you to use AI. In particular we suggest asking GPT-5 or Claude for a “critical code review” of your solution before you return it to us. This models how we work in the real world, we want to test your architectural skills, not nitpick small details that AI can help you with.