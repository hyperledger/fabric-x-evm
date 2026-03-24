// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

contract Counter {
    uint public count; // State variable to store the counter's value

    // Function to increment the counter
    function increment() public {
        count++; // Increases the count by 1
    }

    // Function to decrement the counter
    function decrement() public {
        count--; // Decreases the count by 1
    }

    // Function to get the current counter value
    function getCount() public view returns (uint) {
        return count; // Returns the current value of count
    }
}
